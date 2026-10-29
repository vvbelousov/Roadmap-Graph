// @vitest-environment happy-dom
import {
  render,
  screen,
  fireEvent,
  waitFor,
  cleanup,
  within,
} from "@testing-library/svelte";
import { afterEach, describe, it, expect, vi } from "vitest";
import TrackerPanel from "./TrackerPanel.svelte";
afterEach(cleanup);
function state() {
  return {
    config: {
      enabled: false,
      org_id: "",
      org_header: "X-Org-ID",
      auth: "OAuth",
      poll_interval_seconds: 300,
      selections: [],
      fields: {
        title: { field: "summary", writable: true, codec: "string" },
        planned_date: { field: "dueDate", writable: true, codec: "date" },
      },
      statuses: {},
      write_statuses: {},
      links: [],
    },
    records: [],
    outbox: [],
    last_success: "",
    last_error: "",
    running: false,
  } as any;
}
const projects = [
  {
    id: "p1",
    name: "Core",
    slug: "core",
    description: "",
    color: "#60a5fa",
    icon: "",
    sort_order: 0,
  },
];
describe("Tracker UI", () => {
  it("explains an issue key used as a queue in Russian", async () => {
    const data = state();
    data.last_error =
      "Queue selection contains issue key AP-270; use queue AP or search query Key: AP-270";
    const api = vi.fn(async (..._args: any[]) => structuredClone(data));
    render(TrackerPanel, { props: { api, lang: "ru", projects } });
    await screen.findByText(
      "AP-270 — ключ задачи. Укажите очередь AP или поисковый запрос Key: AP-270",
    );
  });

  it("configures a connection, previews and confirms import without browser credentials", async () => {
    const data = state();
    const change = vi.fn();
    const api = vi.fn(async (path: string, method = "GET", body?: any) => {
      if (path === "/tracker/config") {
        data.config = JSON.parse(JSON.stringify(body));
        return body;
      }
      if (path === "/tracker/preview")
        return {
          id: "preview1",
          projects: [],
          milestones: [
            {
              issue_key: "CORE-1",
              milestone: { title: "Core V2", planned_date: "2027-01-02" },
            },
          ],
          links: [],
          skipped: [
            {
              issue_key: "CORE-2",
              reason: "Use ISO YYYY-MM-DD calendar dates",
            },
          ],
          warnings: [],
        };
      return structuredClone(data);
    });
    render(TrackerPanel, {
      props: { api, lang: "en", projects, onchange: change },
    });
    await fireEvent.input(await screen.findByLabelText("Organization ID"), {
      target: { value: "org" },
    });
    await fireEvent.click(screen.getByLabelText("Enable integration"));
    await fireEvent.click(
      screen.getByRole("button", { name: "Add selection" }),
    );
    await fireEvent.input(screen.getByLabelText("Queue key"), {
      target: { value: "CORE" },
    });
    await fireEvent.click(
      screen.getByRole("button", { name: "Save Tracker settings" }),
    );
    await waitFor(() =>
      expect(api).toHaveBeenCalledWith(
        "/tracker/config",
        "PUT",
        expect.objectContaining({
          org_id: "org",
          enabled: true,
          selections: [{ queue: "CORE", query: "", project_id: "p1" }],
        }),
      ),
    );
    await waitFor(() =>
      expect(
        screen
          .getByRole("button", { name: "Preview import" })
          .hasAttribute("disabled"),
      ).toBe(false),
    );
    await fireEvent.click(
      screen.getByRole("button", { name: "Preview import" }),
    );
    await screen.findByText("CORE-1: Core V2 — 2027-01-02");
    expect(api.mock.calls.some((c) => c[0] === "/tracker/import")).toBe(false);
    await waitFor(() =>
      expect(
        screen
          .getByRole("button", { name: "Confirm import" })
          .hasAttribute("disabled"),
      ).toBe(false),
    );
    await fireEvent.click(
      screen.getByRole("button", { name: "Confirm import" }),
    );
    await waitFor(() =>
      expect(api).toHaveBeenCalledWith("/tracker/import", "POST", {
        preview_id: "preview1",
      }),
    );
    expect(change).toHaveBeenCalled();
    expect(screen.queryByLabelText(/token/i)).toBeNull();
  });
  it("maps discovered Tracker statuses explicitly and reruns preview", async () => {
    const data = state();
    data.config.enabled = true;
    data.config.fields.planned_date.field = "duedatecustomer";
    let previews = 0;
    const api = vi.fn(async (path: string, method = "GET", body?: any) => {
      if (path === "/tracker/config") {
        data.config = JSON.parse(JSON.stringify(body));
        return data.config;
      }
      if (path === "/tracker/preview") {
        previews++;
        return previews === 1
          ? {
              id: "blocked",
              projects: [],
              milestones: [],
              links: [],
              warnings: [],
              unmapped_statuses: [
                { key: "inProgress", name: "В работе", count: 2 },
              ],
              skipped: [
                {
                  issue_key: "AP-270",
                  reason:
                    "unmapped Tracker status: inProgress; Missing planned date: Tracker field duedatecustomer is empty",
                  reasons: [
                    "unmapped Tracker status: inProgress",
                    "Missing planned date: Tracker field duedatecustomer is empty",
                  ],
                },
              ],
            }
          : {
              id: "fixed",
              projects: [],
              milestones: [
                {
                  issue_key: "AP-270",
                  milestone: { title: "Epic", planned_date: "2027-04-12" },
                },
              ],
              links: [],
              warnings: [],
              skipped: [],
              unmapped_statuses: [],
            };
      }
      return structuredClone(data);
    });
    render(TrackerPanel, { props: { api, lang: "ru", projects } });
    const preview = await screen.findByRole("button", {
      name: "Предпросмотр импорта",
    });
    await fireEvent.click(preview);
    const select = await screen.findByRole("combobox", {
      name: "Статус дорожной карты: В работе (inProgress)",
    });
    await screen.findByText(
      "Нет плановой даты: поле Трекера duedatecustomer пустое. Заполните его или выберите другое поле даты.",
    );
    const save = screen.getByRole("button", {
      name: "Сохранить статусы и повторить предпросмотр",
    });
    expect(save.hasAttribute("disabled")).toBe(true);
    expect(
      screen
        .getByRole("button", { name: "Подтвердить импорт" })
        .hasAttribute("disabled"),
    ).toBe(true);
    await fireEvent.change(select, { target: { value: "in_progress" } });
    await waitFor(() => expect(save.hasAttribute("disabled")).toBe(false));
    await fireEvent.click(save);
    await screen.findByText("AP-270: Epic — 2027-04-12");
    expect(api).toHaveBeenCalledWith(
      "/tracker/config",
      "PUT",
      expect.objectContaining({
        statuses: { inProgress: "in_progress" },
        write_statuses: {},
        fields: expect.objectContaining({
          planned_date: expect.objectContaining({ field: "duedatecustomer" }),
        }),
      }),
    );
    expect(api.mock.calls.some((call) => call[0] === "/tracker/import")).toBe(
      false,
    );
  });
  it("persists corrected status keys and field flags and enforces one outgoing target", async () => {
    const data = state();
    data.config.enabled = true;
    data.config.fields.status = {
      field: "status",
      codec: "status",
      writable: false,
    };
    data.config.statuses = {
      "In Progress": "in_progress",
      review: "in_progress",
    };
    data.config.write_statuses = { in_progress: "In Progress" };
    const api = vi.fn(async (path: string, method = "GET", body?: any) => {
      if (path === "/tracker/config") {
        data.config = JSON.parse(JSON.stringify(body));
        return structuredClone(data.config);
      }
      return structuredClone(data);
    });
    const first = render(TrackerPanel, {
      props: { api, lang: "en", projects },
    });
    await screen.findByLabelText("Use for outgoing changes: In Progress");
    const keys = screen.getAllByLabelText("Tracker status key");
    await fireEvent.input(keys[0], { target: { value: "inProgress" } });
    await fireEvent.click(
      screen.getByLabelText("Use for outgoing changes: review"),
    );
    expect(
      (
        screen.getByLabelText(
          "Use for outgoing changes: inProgress",
        ) as HTMLInputElement
      ).checked,
    ).toBe(false);
    await fireEvent.click(
      screen.getByLabelText("Use for outgoing changes: inProgress"),
    );
    expect(
      (
        screen.getByLabelText(
          "Use for outgoing changes: review",
        ) as HTMLInputElement
      ).checked,
    ).toBe(false);
    await fireEvent.click(
      screen.getByRole("button", { name: "Field and dependency mappings" }),
    );
    const statusGroup = screen.getByRole("group", { name: "Status" });
    await fireEvent.click(
      within(statusGroup).getByRole("checkbox", {
        name: "Allow outgoing changes",
      }),
    );
    await fireEvent.click(
      screen.getByRole("button", { name: "Save Tracker settings" }),
    );
    await screen.findByText("Tracker settings saved.");
    expect(data.config.statuses).toEqual({
      inProgress: "in_progress",
      review: "in_progress",
    });
    expect(data.config.write_statuses).toEqual({ in_progress: "inProgress" });
    expect(data.config.fields.status.writable).toBe(true);
    first.unmount();
    render(TrackerPanel, { props: { api, lang: "en", projects } });
    const saved = await screen.findByLabelText(
      "Use for outgoing changes: inProgress",
    );
    expect((saved as HTMLInputElement).checked).toBe(true);
    expect(
      (
        screen.getByLabelText(
          "Use for outgoing changes: review",
        ) as HTMLInputElement
      ).checked,
    ).toBe(false);
    await fireEvent.click(
      screen.getByRole("button", { name: "Field and dependency mappings" }),
    );
    expect(
      (
        within(screen.getByRole("group", { name: "Status" })).getByRole(
          "checkbox",
          { name: "Allow outgoing changes" },
        ) as HTMLInputElement
      ).checked,
    ).toBe(true);
  });
  it("shows separate sync conflicts and sends explicit per-field choices", async () => {
    const data = state();
    data.config.enabled = true;
    data.records = [
      {
        local_id: "m1",
        issue_key: "CORE-1",
        url: "https://tracker.yandex.ru/CORE-1",
        state: "conflict",
        conflicts: {
          title: {
            baseline: "Old",
            local: "Local title",
            remote: "Tracker title",
          },
        },
      },
    ];
    const api = vi.fn(async (..._args: any[]) => structuredClone(data));
    render(TrackerPanel, { props: { api, lang: "en", milestoneID: "m1" } });
    await screen.findByRole("heading", { name: "Synchronization conflict" });
    const resolve = screen.getByRole("button", {
      name: "Resolve selected values",
    });
    expect(resolve.hasAttribute("disabled")).toBe(true);
    await fireEvent.click(
      screen.getByLabelText("Keep Tracker value: Tracker title"),
    );
    await fireEvent.click(resolve);
    await waitFor(() =>
      expect(api).toHaveBeenCalledWith(
        "/tracker/milestones/m1/resolve",
        "POST",
        { choices: { title: "remote" } },
      ),
    );
  });
  it("publishes only through the explicit destination action and localizes controls", async () => {
    const data = state();
    data.config.enabled = true;
    data.config.selections = [{ queue: "CORE", project_id: "p1" }];
    const api = vi.fn(async (..._args: any[]) => structuredClone(data));
    render(TrackerPanel, { props: { api, lang: "ru", milestoneID: "m1" } });
    const publish = await screen.findByRole("button", {
      name: "Опубликовать в Трекере",
    });
    expect(api.mock.calls.every((c) => c.length === 1)).toBe(true);
    await fireEvent.click(publish);
    await waitFor(() =>
      expect(api).toHaveBeenCalledWith(
        "/tracker/milestones/m1/publish",
        "POST",
        { selection: 0 },
      ),
    );
  });
});
