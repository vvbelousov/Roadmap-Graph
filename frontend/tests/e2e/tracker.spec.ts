import { test, expect } from "@playwright/test";
// Browser-only API fixtures; backend integration is separately tested against httptest HTTP servers.
test("Tracker connection setup, confirmed import and explicit conflict choice", async ({
  page,
}) => {
  const password = process.env.ROADMAP_TEST_PASSWORD;
  test.skip(!password, "Run against an isolated local test instance.");
  const project = {
    id: "p1",
    revision: 1,
    name: "Core",
    slug: "core",
    description: "",
    icon: "",
    color: "#60a5fa",
    sort_order: 0,
  };
  const milestone = {
    id: "m1",
    revision: 1,
    project_id: "p1",
    title: "Core V2",
    description: "",
    milestone_type: "release",
    version: "",
    planned_date: "2027-02-01",
    completed_date: "",
    status: "planned",
    owner: "",
    external_url: "https://tracker.yandex.ru/CORE-1",
  };
  let imported = false;
  let connection: any = {
    enabled: false,
    org_id: "",
    org_header: "X-Org-ID",
    auth: "OAuth",
    poll_interval_seconds: 300,
    revision: 0,
  };
  let tracker: any = {
    config: {
      enabled: false,
      revision: 0,
      selections: [],
      fields: {
        title: { field: "summary", codec: "string", writable: true },
        planned_date: { field: "dueDate", codec: "date", writable: true },
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
    connection,
  };
  await page.route("**/api/graph", (r) =>
    r.fulfill({
      json: {
        projects: [project],
        milestones: imported ? [milestone] : [],
        dependencies: [],
      },
    }),
  );
  await page.route("**/api/analysis/conflicts", (r) => r.fulfill({ json: [] }));
  await page.route("**/api/milestones/m1/impact", (r) =>
    r.fulfill({ json: { direct: [], transitive: [] } }),
  );
  const handler = async (route: any) => {
    const path = new URL(route.request().url()).pathname;
    if (path === "/api/tracker/config") {
      connection = {
        ...route.request().postDataJSON(),
        revision: connection.revision + 1,
      };
      tracker.connection = connection;
      await route.fulfill({ json: connection });
      return;
    }
    if (path.endsWith("/config")) {
      tracker.config = {
        ...route.request().postDataJSON(),
        revision: tracker.config.revision + 1,
      };
      await route.fulfill({ json: tracker.config });
      return;
    }
    if (path.endsWith("/preview")) {
      await route.fulfill({
        json: {
          id: "preview1",
          projects: [],
          milestones: [{ issue_key: "CORE-1", milestone }],
          links: [],
          skipped: [],
          warnings: [],
        },
      });
      return;
    }
    if (path.endsWith("/import")) {
      imported = true;
      tracker.records = [
        {
          local_id: "m1",
          local_revision: 1,
          issue_key: "CORE-1",
          url: milestone.external_url,
          state: "conflict",
          error: "",
          conflicts: {
            title: {
              baseline: "Old title",
              local: "Core V2",
              remote: "Tracker V2",
            },
          },
        },
      ];
      await route.fulfill({ json: { imported: true } });
      return;
    }
    if (path.endsWith("/resolve")) {
      expect(route.request().postDataJSON()).toEqual({
        choices: { title: "remote" },
        revision: 1,
      });
      tracker.records[0].state = "pending_upload";
      tracker.records[0].conflicts = {};
      await route.fulfill({ json: { resolved: true } });
      return;
    }
    if (path === "/api/tracker") {
      await route.fulfill({
        json: {
          config: connection,
          projects: [
            { project_id: "p1", name: "Core", enabled: tracker.config.enabled },
          ],
          token_available: true,
          records: [],
          outbox: [],
        },
      });
      return;
    }
    await route.fulfill({ json: tracker });
  };
  await page.route("**/api/tracker**", handler);
  await page.route("**/api/projects/p1/tracker**", handler);
  await page.goto("/");
  await page.getByLabel("Password", { exact: true }).fill(password!);
  await page.getByRole("button", { name: /^Sign in/ }).click();
  await page.getByRole("button", { name: "Settings", exact: true }).click();
  await page.getByLabel("Organization ID", { exact: true }).fill("org");
  await page.getByLabel("Enable integration").check();
  await page.getByRole("button", { name: "Save Tracker settings" }).click();
  await expect(page.getByText("Tracker settings saved.")).toBeVisible();
  await page.getByRole("button", { name: "Test saved connection" }).click();
  await expect(page.getByText("Connection succeeded.")).toBeVisible();
  await expect(page.getByLabel("Queue key")).toHaveCount(0);
  await page.getByRole("button", { name: "Core →" }).click();
  await expect(
    page.getByRole("heading", { name: "Project settings: Core" }),
  ).toBeVisible();
  await page.getByLabel("Enable this project's synchronization").check();
  await page.getByRole("button", { name: "Add selection" }).click();
  await page.getByLabel("Queue key").fill("CORE");
  await page.getByRole("button", { name: "Save Tracker settings" }).click();
  await expect(page.getByText("Tracker settings saved.")).toBeVisible();
  await page
    .getByRole("button", { name: "Preview import", exact: true })
    .click();
  await expect(page.getByText("CORE-1: Core V2 — 2027-02-01")).toBeVisible();
  expect(imported).toBe(false);
  await page.getByRole("button", { name: "Confirm import" }).click();
  await expect(page.getByText("Import applied.")).toBeVisible();
  await page.getByRole("button", { name: /Overview/ }).click();
  const marker = page.getByRole("button", { name: "Core V2", exact: true });
  await marker.scrollIntoViewIfNeeded();
  await marker.locator("circle").nth(1).click();
  await expect(
    page.getByRole("heading", { name: "Synchronization conflict" }),
  ).toBeVisible();
  await page.getByLabel("Keep Tracker value: Tracker V2").check();
  await page.getByRole("button", { name: "Resolve selected values" }).click();
  await expect(page.getByText("Synchronization: Pending upload")).toBeVisible();
  await page.getByRole("button", { name: "Theme", exact: true }).click();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
  await page.getByRole("button", { name: "Settings", exact: true }).click();
  await page
    .getByRole("combobox", { name: "Language", exact: true })
    .selectOption("ru");
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(
    page.getByRole("heading", { name: "Яндекс Трекер", exact: true }),
  ).toBeVisible();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
});

test("project settings save independently and warn before discarding another project draft", async ({
  page,
}) => {
  const password = process.env.ROADMAP_TEST_PASSWORD;
  test.skip(!password, "Use an isolated local instance.");
  await page.goto("/");
  await page.getByLabel("Password", { exact: true }).fill(password!);
  await page.getByRole("button", { name: /^Sign in/ }).click();
  await expect(
    page.getByRole("button", { name: "Add project", exact: true }),
  ).toBeVisible();
  const session = await (await page.request.get("/api/auth/session")).json();
  const suffix = Date.now();
  const projects = [];
  for (const name of [`Project A ${suffix}`, `Project B ${suffix}`]) {
    const response = await page.request.post("/api/projects", {
      headers: { "X-CSRF-Token": session.csrf_token },
      data: {
        name,
        slug: name.toLowerCase().replaceAll(" ", "-"),
        color: "#60a5fa",
      },
    });
    expect(response.ok(), await response.text()).toBeTruthy();
    projects.push(await response.json());
  }
  await page.reload();
  await page
    .getByRole("button", {
      name: `Project settings ${projects[0].name}`,
      exact: true,
    })
    .click();
  await page
    .getByRole("button", { name: "Field and dependency mappings" })
    .click();
  await page
    .getByRole("group", { name: "Planned date", exact: true })
    .getByLabel("Tracker field ID")
    .fill("customDateA");
  await page.getByRole("button", { name: "Save Tracker settings" }).click();
  await expect(page.getByText("Tracker settings saved.")).toBeVisible();
  await page
    .getByRole("button", {
      name: `Project settings ${projects[1].name}`,
      exact: true,
    })
    .click();
  await page
    .getByRole("button", { name: "Field and dependency mappings" })
    .click();
  const date = page
    .getByRole("group", { name: "Planned date", exact: true })
    .getByLabel("Tracker field ID");
  await expect(date).toHaveValue("dueDate");
  await date.fill("unsavedDateB");
  page.once("dialog", (dialog) => dialog.dismiss());
  await page
    .getByRole("button", {
      name: `Project settings ${projects[0].name}`,
      exact: true,
    })
    .click();
  await expect(date).toHaveValue("unsavedDateB");
  page.once("dialog", (dialog) => dialog.accept());
  await page
    .getByRole("button", {
      name: `Project settings ${projects[0].name}`,
      exact: true,
    })
    .click();
  await page
    .getByRole("button", { name: "Field and dependency mappings" })
    .click();
  await expect(
    page
      .getByRole("group", { name: "Planned date", exact: true })
      .getByLabel("Tracker field ID"),
  ).toHaveValue("customDateA");
  await page.setViewportSize({ width: 390, height: 844 });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
  const a = await (
    await page.request.get(`/api/projects/${projects[0].id}/tracker`)
  ).json();
  const b = await (
    await page.request.get(`/api/projects/${projects[1].id}/tracker`)
  ).json();
  expect(a.config.fields.planned_date.field).toBe("customDateA");
  expect(b.configured).toBe(false);
});
