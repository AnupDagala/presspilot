import { test, expect } from "@playwright/test";
async function submit(page: import("@playwright/test").Page) {
  await page.goto("/");
  await page.getByRole("button", { name: "Start isolated demo" }).click();
  await page.getByRole("button", { name: "New case" }).click();
  await page.getByRole("button", { name: "Submit case", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Cancel order", exact: true }),
  ).toBeVisible({ timeout: 30000 });
  await expect(page.getByText("Order snapshot", { exact: true })).toBeVisible();
}
test("submit, inspect evidence and approve exact cancellation", async ({
  page,
}) => {
  await submit(page);
  await expect(
    page.getByRole("button", { name: "Approve exact action" }),
  ).toBeDisabled();
  await page.getByLabel("Demo role").selectOption("approver");
  await page.getByRole("button", { name: "Approve exact action" }).click();
  await expect(page.getByText("action executed", { exact: true })).toBeVisible({
    timeout: 20000,
  });
  await expect(page.getByText("cancelled", { exact: true })).toBeVisible();
});
test("rejection escalates without mutating order", async ({ page }) => {
  await submit(page);
  await page.getByLabel("Demo role").selectOption("approver");
  await page.getByRole("button", { name: "Reject", exact: true }).click();
  await expect(page.getByText("action rejected", { exact: true })).toBeVisible({
    timeout: 20000,
  });
  await expect(page.getByText("queued", { exact: true })).toBeVisible();
});
test("outdated approval is invalidated and production order remains unchanged", async ({
  page,
}) => {
  await submit(page);
  await page.getByLabel("Demo role").selectOption("administrator");
  await page.getByRole("button", { name: "Simulate production start" }).click();
  await expect(page.getByText("production", { exact: true })).toBeVisible();
  await page.getByLabel("Demo role").selectOption("approver");
  await page.getByRole("button", { name: "Approve exact action" }).click();
  await expect(
    page.getByText("proposal invalidated", { exact: true }),
  ).toBeVisible({ timeout: 20000 });
  await expect(page.getByText("case escalated", { exact: true })).toBeVisible({
    timeout: 20000,
  });
  await expect(page.getByText("production", { exact: true })).toBeVisible();
});
test("responsive console remains usable with a keyboard", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/");
  await page.getByRole("button", { name: "Start isolated demo" }).focus();
  await page.keyboard.press("Enter");
  await expect(page.getByRole("button", { name: "New case" })).toBeVisible();
  await expect(page.locator("body")).toHaveJSProperty("scrollWidth", 390);
});

test("administrator review withdraws a pending proposal without an order effect", async ({
  page,
}) => {
  await submit(page);
  await expect(
    page.getByRole("button", { name: "Request administrator review" }),
  ).toHaveCount(0);
  await page.getByLabel("Demo role").selectOption("administrator");
  await page
    .getByRole("button", { name: "Request administrator review" })
    .click();
  await expect(
    page.getByText("administrator review requested", { exact: true }),
  ).toBeVisible({ timeout: 20000 });
  await expect(page.getByText("queued", { exact: true })).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Approve exact action" }),
  ).toHaveCount(0);
});

test("evaluation modes distinguish measured mocked runs from unverified live execution", async ({
  page,
}) => {
  await page.goto("/");
  await page.getByRole("button", { name: "Start isolated demo" }).click();
  await page.getByRole("button", { name: "Evaluations", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Measured baseline evaluation" }),
  ).toBeVisible();
  await page.getByLabel("Evaluation mode").selectOption("mocked");
  await expect(
    page.getByRole("heading", { name: "Measured mocked evaluation" }),
  ).toBeVisible();
  await page.getByLabel("Evaluation mode").selectOption("live");
  await expect(
    page.getByRole("heading", { name: "No measured report loaded." }),
  ).toBeVisible();
});
