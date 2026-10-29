// Explicit mock-network DOM integration tests. Production always uses the Go API.
// @vitest-environment happy-dom
import {
  render,
  screen,
  fireEvent,
  waitFor,
  cleanup,
} from "@testing-library/svelte";
import { beforeEach, afterEach, describe, it, expect, vi } from "vitest";
import App from "./App.svelte";
import { flushSync } from "svelte";
const project = (
  id: string,
  name: string,
  color: string,
  sort_order: number,
) => ({
  id,
  name,
  slug: name.toLowerCase(),
  description: name + " platform",
  color,
  icon: "",
  sort_order,
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
});
const milestone = (
  id: string,
  project_id: string,
  title: string,
  planned_date: string,
) => ({
  id,
  project_id,
  title,
  planned_date,
  status: "planned",
  milestone_type: "release",
  description: "Release description",
  version: "2.0",
  completed_date: "",
  owner: "Demo owner",
  external_url: "",
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
});
const fixture = {
  projects: [
    project("common-module", "COMMON MODULE", "#50c79b", 0),
    project("project-x", "PROJECT X", "#7b9cff", 1),
  ],
  milestones: [
    milestone(
      "common-module2",
      "common-module",
      "COMMON MODULE V2",
      "2027-01-20",
    ),
    milestone("project-x2", "project-x", "PROJECT X V2", "2027-01-10"),
  ],
  dependencies: [
    {
      id: "edge1",
      prerequisite_milestone_id: "common-module2",
      dependent_milestone_id: "project-x2",
      dependency_type: "requires",
    },
  ],
};
let calls: { url: string; method: string; body: any; headers: any }[];
beforeEach(() => {
  localStorage.clear();
  calls = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: string, init: RequestInit = {}) => {
      const url = String(input),
        method = init.method || "GET",
        body = init.body ? JSON.parse(String(init.body)) : undefined;
      calls.push({ url, method, body, headers: init.headers });
      let data: any = {};
      if (url.endsWith("/auth/session"))
        data = { authenticated: true, csrf_token: "csrf-test" };
      else if (url.endsWith("/graph")) data = structuredClone(fixture);
      else if (url.endsWith("/analysis/conflicts"))
        data = [
          {
            prerequisite_milestone_id: "common-module2",
            dependent_milestone_id: "project-x2",
            days: 10,
            category: "schedule_conflict",
          },
        ];
      else if (url.endsWith("/settings")) data = {};
      else if (url.endsWith("/impact"))
        data = { direct: ["project-x2"], transitive: [], conflicts: [] };
      else if (method === "PATCH") data = { ...fixture.milestones[0], ...body };
      return { ok: true, status: 200, json: async () => data } as Response;
    }),
  );
  HTMLDialogElement.prototype.showModal = function () {
    this.setAttribute("open", "");
  };
  HTMLDialogElement.prototype.close = function () {
    this.removeAttribute("open");
  };
});
afterEach(() => {
  cleanup();
  fixture.milestones[0].status = "planned";
  vi.unstubAllGlobals();
});
async function boot() {
  const result = render(App);
  await new Promise((resolve) => setTimeout(resolve, 0));
  flushSync();
  await screen.findByRole("heading", { name: /Portfolio overview/ });
  await waitFor(() =>
    expect(
      result.container.querySelectorAll('[data-testid="milestone"]').length,
    ).toBe(2),
  );
  return result;
}
describe("App DOM integration", () => {
  it("diagnoses hidden milestones and Fit restores the date window", async () => {
    window.history.replaceState({}, "", "/?debug=1");
    try {
      await boot();
      const diagnostics = screen.getByTestId("timeline-diagnostics");
      expect(diagnostics.textContent).toContain('"loaded_milestones": 2');
      await fireEvent.input(screen.getByLabelText("From", { exact: true }), {
        target: { value: "2030-01-01" },
      });
      await screen.findByText(
        "Milestones are outside the selected date range.",
      );
      expect(diagnostics.textContent).toContain('"in_date_window": 0');
      await fireEvent.click(screen.getAllByRole("button", { name: "Fit" })[0]);
      await waitFor(() =>
        expect(diagnostics.textContent).toContain('"in_date_window": 2'),
      );
      expect(
        (screen.getByLabelText("From", { exact: true }) as HTMLInputElement)
          .value,
      ).toBe("");
    } finally {
      window.history.replaceState({}, "", "/");
    }
  });
  it("filters project lanes and restores them using the actual sidebar control", async () => {
    const { container } = await boot();
    await fireEvent.click(
      screen.getByRole("checkbox", { name: "COMMON MODULE" }),
    );
    await waitFor(() =>
      expect(
        container.querySelectorAll('[data-testid="milestone"]').length,
      ).toBe(1),
    );
    expect(container.querySelector('[data-id="project-x2"]')).not.toBeNull();
    await fireEvent.click(
      screen.getByRole("checkbox", { name: "COMMON MODULE" }),
    );
    await waitFor(() =>
      expect(
        container.querySelectorAll('[data-testid="milestone"]').length,
      ).toBe(2),
    );
  });
  it("selects milestone, loads impact, opens details, and highlights permanent dependency", async () => {
    const { container } = await boot();
    const edge = container.querySelector('[data-dependency-id="edge1"]')!;
    expect(edge.getAttribute("opacity")).toBe(".26");
    await fireEvent.click(
      screen.getByRole("button", { name: "COMMON MODULE V2" }),
    );
    const details = await screen.findByRole("complementary", {
      name: "Milestone details",
    });
    expect(details.textContent).toContain("Release description");
    expect(details.textContent).toContain("PROJECT X V2");
    await waitFor(() =>
      expect(edge.getAttribute("data-highlighted")).toBe("true"),
    );
    expect(
      calls.some((c) => c.url === "/api/milestones/common-module2/impact"),
    ).toBe(true);
  });
  it("shows date tooltip and transitive highlighting on pointer entry", async () => {
    const { container } = await boot();
    await fireEvent.mouseEnter(
      screen.getByRole("button", { name: "COMMON MODULE V2" }),
      { clientX: 100, clientY: 100 },
    );
    const tooltip = await screen.findByRole("tooltip");
    expect(tooltip.textContent).toContain("20 Jan 2027");
    expect(tooltip.textContent).toContain("COMMON MODULE V2");
    expect(
      container
        .querySelector('[data-dependency-id="edge1"]')
        ?.getAttribute("data-highlighted"),
    ).toBe("true");
    await fireEvent.mouseLeave(
      screen.getByRole("button", { name: "COMMON MODULE V2" }),
    );
    await waitFor(() => expect(screen.queryByRole("tooltip")).toBeNull());
  });
  it("switches theme and language and persists preferences", async () => {
    await boot();
    await fireEvent.click(screen.getByRole("button", { name: "Theme" }));
    await waitFor(() =>
      expect(document.documentElement.dataset.theme).toBe("light"),
    );
    expect(localStorage.getItem("rg.theme")).toBe("light");
    await fireEvent.click(screen.getByRole("button", { name: "Language" }));
    await fireEvent.click(screen.getByRole("button", { name: "Настройки" }));
    await screen.findByRole("heading", { name: "Настройки" });
    expect(document.documentElement.lang).toBe("ru");
    expect(localStorage.getItem("rg.lang")).toBe("ru");
  });
  it("requires title/date in milestone form and strips immutable metadata from edit PATCH", async () => {
    await boot();
    await fireEvent.click(
      screen.getByRole("button", { name: /Add milestone/ }),
    );
    const title = (await screen.findByLabelText("Title")) as HTMLInputElement;
    const planned = screen.getByLabelText("Planned date") as HTMLInputElement;
    expect(title.required).toBe(true);
    expect(title.checkValidity()).toBe(false);
    expect(planned.required).toBe(true);
    await fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    await fireEvent.click(
      screen.getByRole("button", { name: "COMMON MODULE V2" }),
    );
    await screen.findByRole("complementary", { name: "Milestone details" });
    await fireEvent.click(screen.getByRole("button", { name: /Edit$/ }));
    await fireEvent.input(await screen.findByLabelText("Title"), {
      target: { value: "COMMON MODULE V2 edited" },
    });
    await fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
    await waitFor(() =>
      expect(calls.some((c) => c.method === "PATCH")).toBe(true),
    );
    const mutation = calls.find((c) => c.method === "PATCH")!;
    expect(mutation.url).toBe("/api/milestones/common-module2");
    expect(mutation.body.title).toBe("COMMON MODULE V2 edited");
    expect(mutation.body).not.toHaveProperty("id");
    expect(mutation.body).not.toHaveProperty("created_at");
    expect(mutation.body).not.toHaveProperty("updated_at");
    expect(mutation.headers["X-CSRF-Token"]).toBe("csrf-test");
  });
  it("requires an explicit actual completion date for completed status", async () => {
    fixture.milestones[0].status = "completed";
    await boot();
    await fireEvent.click(
      screen.getByRole("button", { name: "COMMON MODULE V2" }),
    );
    await screen.findByRole("complementary", { name: "Milestone details" });
    await fireEvent.click(screen.getByRole("button", { name: /Edit$/ }));
    const completed = (await screen.findByLabelText(
      "Completion date",
    )) as HTMLInputElement;
    expect(completed.required).toBe(true);
    expect(completed.value).toBe("");
    expect(completed.checkValidity()).toBe(false);
  });
  it("opens login when session is unauthenticated and submits password to API", async () => {
    vi.mocked(fetch).mockImplementation(
      async (input: any, init?: RequestInit) =>
        new Response(
          JSON.stringify(
            String(input).endsWith("/auth/session")
              ? { authenticated: false }
              : String(input).endsWith("/auth/login")
                ? { authenticated: true, csrf_token: "login-csrf" }
                : String(input).endsWith("/graph")
                  ? fixture
                  : String(input).endsWith("/analysis/conflicts")
                    ? []
                    : {},
          ),
          { status: 200 },
        ),
    );
    render(App);
    await screen.findByRole("heading", { name: "Sign in" });
    await fireEvent.input(screen.getByLabelText("Password"), {
      target: { value: "test-password-value" },
    });
    await fireEvent.click(screen.getByRole("button", { name: /Sign in/ }));
    await screen.findByRole("heading", { name: /Portfolio overview/ });
    const request = vi
      .mocked(fetch)
      .mock.calls.find((c) => c[0] === "/api/auth/login")!;
    expect(JSON.parse(String(request[1]?.body))).toEqual({
      password: "test-password-value",
    });
  });
});
