import { expect, test } from "@playwright/test";

test("root shows the add form", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("button", { name: "Download" })).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Paste from clipboard" })
  ).toBeVisible();
});

test("history page renders the status table", async ({ page }) => {
  await page.goto("/history");
  await expect(page.getByRole("searchbox", { name: "Search history" })).toBeVisible();
  await expect(page.getByRole("columnheader", { name: "Name" })).toBeVisible();
  await expect(page.getByRole("columnheader", { name: "URL" })).toBeVisible();
});

test("history search updates and clears the URL", async ({ page }) => {
  await page.route("**/api/history?**", async (route) => {
    await route.fulfill({
      json: { records: [], next_cursor: "" },
    });
  });

  const initialHistoryResponse = page.waitForResponse(
    (response) =>
      response.url().includes("/api/history") &&
      response.url().includes("limit=50")
  );
  await page.goto("/history");
  await initialHistoryResponse;

  const search = page.getByRole("searchbox", { name: "Search history" });
  await search.fill("soft light");

  const searchRequest = page.waitForRequest(
    (request) =>
      request.url().includes("/api/history") &&
      request.url().includes("q=soft+light")
  );
  await search.press("Enter");
  await searchRequest;

  await expect(page).toHaveURL(/\/history\?q=soft\+light$/);

  await page.getByRole("button", { name: "Clear search" }).click();
  await expect(page).toHaveURL(/\/history\/?$/);
});

test("sidebar navigates between pages", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("link", { name: "History" }).click();
  await expect(page).toHaveURL(/\/history\/?$/);
  await page.getByRole("link", { name: "Downloads" }).click();
  await expect(page).toHaveURL(/\/$/);
});
