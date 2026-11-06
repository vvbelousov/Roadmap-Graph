import { test, expect } from "@playwright/test";

test("two tabs preserve a stale draft and review the newer planned date", async ({
  page,
  context,
}) => {
  const password = process.env.ROADMAP_TEST_PASSWORD;
  test.skip(!password, "Run only against an isolated local test database.");
  await page.goto("/");
  await page.getByLabel("Password", { exact: true }).fill(password!);
  await page.getByRole("button", { name: /^Sign in/ }).click();
  await expect(
    page.getByRole("button", { name: "Add project", exact: true }),
  ).toBeVisible();
  const session = await (await page.request.get("/api/auth/session")).json();
  const headers = { "X-CSRF-Token": session.csrf_token };
  const suffix = Date.now().toString();
  const projectResponse = await page.request.post("/api/projects", {
    headers,
    data: {
      name: `Revision ${suffix}`,
      slug: `revision-${suffix}`,
      color: "#60a5fa",
    },
  });
  expect(projectResponse.status()).toBe(201);
  const project = await projectResponse.json();
  try {
    const milestoneResponse = await page.request.post("/api/milestones", {
      headers,
      data: {
        project_id: project.id,
        title: `Concurrent ${suffix}`,
        planned_date: "2027-03-01",
      },
    });
    expect(milestoneResponse.status()).toBe(201);
    const milestone = await milestoneResponse.json();
    const other = await context.newPage();
    for (const tab of [page, other]) {
      await tab.goto("/");
      const marker = tab.getByRole("button", {
        name: milestone.title,
        exact: true,
      });
      await marker.scrollIntoViewIfNeeded();
      await marker.locator("circle").nth(1).click();
      await tab.getByRole("button", { name: /Edit$/ }).click();
      await expect(tab.getByLabel("Title", { exact: true })).toHaveValue(
        milestone.title,
      );
    }
    await page.getByLabel("Planned date", { exact: true }).fill("2027-06-01");
    await page
      .getByRole("button", { name: "Save changes", exact: true })
      .click();
    await expect(page.getByRole("dialog")).not.toBeVisible();
    const draft = `Draft ${suffix}`;
    await other.getByLabel("Title", { exact: true }).fill(draft);
    await other
      .getByRole("button", { name: "Save changes", exact: true })
      .click();
    await expect(other.getByText(/Your draft is preserved/)).toBeVisible();
    await expect(other.getByLabel("Title", { exact: true })).toHaveValue(draft);
    let current = await (
      await page.request.get(`/api/milestones/${milestone.id}`)
    ).json();
    expect(current.planned_date).toBe("2027-06-01");
    expect(current.title).toBe(milestone.title);
    await other
      .getByRole("button", {
        name: "Load current data and review draft",
        exact: true,
      })
      .click();
    await expect(other.getByLabel("Planned date", { exact: true })).toHaveValue(
      "2027-06-01",
    );
    await expect(other.getByLabel("Title", { exact: true })).toHaveValue(draft);
    await other
      .getByRole("button", { name: "Save changes", exact: true })
      .click();
    await expect(other.getByRole("dialog")).not.toBeVisible();
    current = await (
      await page.request.get(`/api/milestones/${milestone.id}`)
    ).json();
    expect(current.title).toBe(draft);
    expect(current.planned_date).toBe("2027-06-01");
    await other.close();
  } finally {
    expect(
      (
        await page.request.delete(`/api/projects/${project.id}`, { headers })
      ).status(),
    ).toBe(204);
  }
});
