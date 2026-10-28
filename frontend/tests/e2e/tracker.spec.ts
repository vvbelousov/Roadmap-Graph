import { test, expect } from "@playwright/test";
// Browser-only API fixtures; backend integration is separately tested against httptest HTTP servers.
test("Tracker connection setup, confirmed import and explicit conflict choice", async ({
  page,
}) => {
  const password = process.env.ROADMAP_TEST_PASSWORD;
  test.skip(!password, "Run against an isolated local test instance.");
  const project = {
    id: "p1",
    name: "Core",
    slug: "core",
    description: "",
    icon: "",
    color: "#60a5fa",
    sort_order: 0,
  };
  const milestone = {
    id: "m1",
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
  let tracker: any = {
    config: {
      enabled: false,
      org_id: "",
      org_header: "X-Org-ID",
      auth: "OAuth",
      poll_interval_seconds: 300,
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
  await page.route("**/api/tracker**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path.endsWith("/config")) {
      tracker.config = route.request().postDataJSON();
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
      });
      tracker.records[0].state = "pending_upload";
      tracker.records[0].conflicts = {};
      await route.fulfill({ json: { resolved: true } });
      return;
    }
    await route.fulfill({ json: tracker });
  });
  await page.goto("/");
  await page.getByLabel("Password", { exact: true }).fill(password!);
  await page.getByRole("button", { name: /^Sign in/ }).click();
  await page.getByRole("button", { name: "Settings", exact: true }).click();
  await page.getByLabel("Organization ID", { exact: true }).fill("org");
  await page.getByLabel("Enable integration").check();
  await page.getByRole("button", { name: "Add selection" }).click();
  await page.getByLabel("Queue key").fill("CORE");
  await page.getByRole("button", { name: "Save Tracker settings" }).click();
  await expect(page.getByText("Tracker settings saved.")).toBeVisible();
  await page.getByRole("button", { name: "Test saved connection" }).click();
  await expect(page.getByText("Connection succeeded.")).toBeVisible();
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
