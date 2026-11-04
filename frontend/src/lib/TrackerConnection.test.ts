// @vitest-environment happy-dom
import {
  render,
  screen,
  fireEvent,
  waitFor,
  cleanup,
} from "@testing-library/svelte";
import { afterEach, it, expect, vi } from "vitest";
import TrackerConnection from "./TrackerConnection.svelte";
afterEach(() => {
  cleanup();
  vi.useRealTimers();
});
it("saves the shared connection before projects exist and contains no editable mappings", async () => {
  let data: any = {
    config: {
      enabled: false,
      org_id: "",
      org_header: "X-Org-ID",
      auth: "OAuth",
      poll_interval_seconds: 300,
      revision: 0,
    },
    projects: [],
    token_available: true,
  };
  const api = vi.fn(async (path: string, method = "GET", body?: any) => {
    if (method === "PUT") {
      data.config = { ...JSON.parse(JSON.stringify(body)), revision: 1 };
      return structuredClone(data.config);
    }
    return structuredClone(data);
  });
  render(TrackerConnection, { props: { api, lang: "en" } });
  await fireEvent.input(await screen.findByLabelText("Organization ID"), {
    target: { value: "org" },
  });
  expect(screen.queryByLabelText("Queue key")).toBeNull();
  expect(screen.queryByText("Status mappings")).toBeNull();
  await fireEvent.click(
    screen.getByRole("button", { name: "Save Tracker settings" }),
  );
  await screen.findByText("Tracker settings saved.");
  expect(api).toHaveBeenCalledWith("/tracker/config", "PUT", {
    enabled: false,
    org_id: "org",
    org_header: "X-Org-ID",
    auth: "OAuth",
    poll_interval_seconds: 300,
    revision: 0,
  });
});
it("preserves unsaved connection edits during background health refresh and links to project settings", async () => {
  vi.useFakeTimers({ toFake: ["setInterval", "clearInterval"] });
  const data: any = {
    config: {
      enabled: false,
      org_id: "original",
      org_header: "X-Org-ID",
      auth: "OAuth",
      poll_interval_seconds: 300,
      revision: 1,
    },
    projects: [{ project_id: "p1", name: "Core", enabled: false }],
    token_available: false,
  };
  const api = vi.fn(async () => structuredClone(data)),
    navigate = vi.fn(),
    dirty = vi.fn();
  render(TrackerConnection, {
    props: { api, lang: "ru", onproject: navigate, onDirtyChange: dirty },
  });
  const org = await screen.findByLabelText("ID организации");
  await fireEvent.input(org, { target: { value: "unsaved" } });
  data.config.org_id = "another session";
  await vi.advanceTimersByTimeAsync(5000);
  expect((org as HTMLInputElement).value).toBe("unsaved");
  await waitFor(() => expect(dirty).toHaveBeenLastCalledWith(true));
  await fireEvent.click(screen.getByRole("button", { name: "Core →" }));
  expect(navigate).toHaveBeenCalledWith("p1");
});
