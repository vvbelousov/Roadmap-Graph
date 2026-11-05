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
      revision: 0,
      selections: [],
      fields: {
        title: { field: "summary", writable: true, codec: "string" },
        planned_date: { field: "dueDate", writable: true, codec: "date" },
      },
      statuses: {},
      write_statuses: {},
      links: [],
    },
    connection: {
      enabled: true,
      org_id: "org",
      org_header: "X-Org-ID",
      auth: "OAuth",
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
    render(TrackerPanel, {
      props: { api, projectID: "p1", lang: "ru", projects },
    });
    await screen.findByText(
      "AP-270 — ключ задачи. Укажите очередь AP или поисковый запрос Key: AP-270",
    );
  });

  it("saves selections only to the containing project and confirms its preview", async () => {
    const data = state();
    const change = vi.fn();
    const api = vi.fn(async (path: string, method = "GET", body?: any) => {
      if (path === "/projects/p1/tracker/config") {
        data.config = JSON.parse(JSON.stringify(body));
        return body;
      }
      if (path === "/projects/p1/tracker/preview")
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
          skipped: [],
          warnings: [],
        };
      return structuredClone(data);
    });
    render(TrackerPanel, {
      props: {
        api,
        lang: "en",
        projects,
        projectID: "p1",
        projectName: "Core",
        onchange: change,
      },
    });
    await fireEvent.click(
      await screen.findByLabelText("Enable this project's synchronization"),
    );
    await fireEvent.click(
      screen.getByRole("button", { name: "Add selection" }),
    );
    await fireEvent.input(screen.getByLabelText("Queue key"), {
      target: { value: "CORE" },
    });
    expect(screen.queryByLabelText("Organization ID")).toBeNull();
    expect(screen.queryByLabelText("Roadmap project")).toBeNull();
    await fireEvent.click(
      screen.getByRole("button", { name: "Save Tracker settings" }),
    );
    await screen.findByText("Tracker settings saved.");
    expect(api).toHaveBeenCalledWith(
      "/projects/p1/tracker/config",
      "PUT",
      expect.objectContaining({
        enabled: true,
        selections: [{ id: expect.any(String), queue: "CORE", query: "" }],
      }),
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
    expect(
      api.mock.calls.some((c) => c[0] === "/projects/p1/tracker/import"),
    ).toBe(false);
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
      expect(api).toHaveBeenCalledWith("/projects/p1/tracker/import", "POST", {
        preview_id: "preview1",
      }),
    );
    expect(change).toHaveBeenCalled();
  });
  it("maps discovered Tracker statuses explicitly and reruns preview", async () => {
    const data = state();
    data.config.enabled = true;
    data.config.fields.planned_date.field = "duedatecustomer";
    let previews = 0;
    const api = vi.fn(async (path: string, method = "GET", body?: any) => {
      if (path === "/projects/p1/tracker/config") {
        data.config = JSON.parse(JSON.stringify(body));
        return data.config;
      }
      if (path === "/projects/p1/tracker/preview") {
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
    render(TrackerPanel, {
      props: { api, projectID: "p1", lang: "ru", projects },
    });
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
      "/projects/p1/tracker/config",
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
      if (path === "/projects/p1/tracker/config") {
        data.config = JSON.parse(JSON.stringify(body));
        return structuredClone(data.config);
      }
      return structuredClone(data);
    });
    const first = render(TrackerPanel, {
      props: { api, projectID: "p1", lang: "en", projects },
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
    render(TrackerPanel, {
      props: { api, projectID: "p1", lang: "en", projects },
    });
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
  it("keeps unsaved custom date edits while project health refreshes", async () => {
    vi.useFakeTimers({ toFake: ["setInterval", "clearInterval"] });
    try {
      const data = state();
      const dirty = vi.fn();
      const api = vi.fn(async () => structuredClone(data));
      render(TrackerPanel, {
        props: {
          api,
          projectID: "p1",
          projectName: "Core",
          lang: "en",
          onDirtyChange: dirty,
        },
      });
      await screen.findByRole("button", {
        name: "Field and dependency mappings",
      });
      await fireEvent.click(
        screen.getByRole("button", { name: "Field and dependency mappings" }),
      );
      const date = within(
        screen.getByRole("group", { name: "Planned date" }),
      ).getByLabelText("Tracker field ID");
      await fireEvent.input(date, { target: { value: "duedatecustomer" } });
      data.config.fields.planned_date.field = "remote edit";
      await vi.advanceTimersByTimeAsync(5000);
      expect((date as HTMLInputElement).value).toBe("duedatecustomer");
      await waitFor(() => expect(dirty).toHaveBeenLastCalledWith(true));
    } finally {
      vi.useRealTimers();
    }
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
    render(TrackerPanel, {
      props: { api, projectID: "p1", lang: "en", milestoneID: "m1" },
    });
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
  it("allows cancelling a retained publication while project synchronization is disabled", async () => {
    const data = state();
    data.outbox = [
      {
        local_id: "m1",
        kind: "publish",
        error: "Pending publication destination requires review",
        attempts: 2,
      },
    ];
    const api = vi.fn(async (path: string, method = "GET", _body?: any) => {
      if (path === "/tracker/milestones/m1/unlink" && method === "POST")
        data.outbox = [];
      return structuredClone(data);
    });
    render(TrackerPanel, {
      props: { api, projectID: "p1", milestoneID: "m1", lang: "en" },
    });
    await fireEvent.click(
      await screen.findByRole("button", { name: "Cancel pending publication" }),
    );
    await waitFor(() =>
      expect(api).toHaveBeenCalledWith(
        "/tracker/milestones/m1/unlink",
        "POST",
        {},
      ),
    );
    await waitFor(() =>
      expect(
        screen.queryByRole("button", { name: "Cancel pending publication" }),
      ).toBeNull(),
    );
  });
  it("publishes only through the explicit destination action and localizes controls", async () => {
    const data = state();
    data.config.enabled = true;
    data.config.selections = [{ id: "sel-core", queue: "CORE", query: "" }];
    const api = vi.fn(async (..._args: any[]) => structuredClone(data));
    render(TrackerPanel, {
      props: { api, projectID: "p1", lang: "ru", milestoneID: "m1" },
    });
    const publish = await screen.findByRole("button", {
      name: "Опубликовать в Трекере",
    });
    expect(api.mock.calls.every((c) => c.length === 1)).toBe(true);
    await fireEvent.click(publish);
    await waitFor(() =>
      expect(api).toHaveBeenCalledWith(
        "/tracker/milestones/m1/publish",
        "POST",
        { selection_id: "sel-core" },
      ),
    );
  });
});
