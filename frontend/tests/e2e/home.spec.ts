import { expect, test } from "@playwright/test";

test("root shows the add form", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("button", { name: "Download" })).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Paste from clipboard" })
  ).toBeVisible();
});

test("downloads alert appears in the desktop lower right", async ({ page }) => {
  await page.route("**/api/downloads/active", async (route) => {
    await route.fulfill({ json: [] });
  });
  await page.route("**/api/queue", async (route) => {
    await route.fulfill({
      json: [
        {
          url: "https://example.com/video",
          status: "rejected",
        },
      ],
    });
  });

  const activeDownloadsResponse = page.waitForResponse((response) =>
    response.url().includes("/api/downloads/active")
  );
  await page.goto("/");
  await activeDownloadsResponse;

  await page.getByRole("textbox").fill("https://example.com/video");
  await page.getByRole("button", { name: "Download" }).click();

  const alert = page
    .getByRole("alert")
    .filter({ hasText: "Link not recognized" });
  await expect(alert).toContainText("Link not recognized");

  const box = await alert.boundingBox();
  const viewport = page.viewportSize();

  expect(box).not.toBeNull();
  expect(viewport).not.toBeNull();
  expect(box!.x).toBeGreaterThan(viewport!.width / 2);
  expect(box!.y).toBeGreaterThan(viewport!.height / 2);
  expect(box!.x + box!.width).toBeLessThanOrEqual(viewport!.width);
  expect(box!.y + box!.height).toBeLessThanOrEqual(viewport!.height);
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
