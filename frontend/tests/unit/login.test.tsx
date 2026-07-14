import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { Login } from "@/components/auth/login";

function renderLogin() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <Login />
    </QueryClientProvider>,
  );
}

describe("Login", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("posts the entered credentials to the login endpoint", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ authenticated: true, username: "root" }),
    });
    vi.stubGlobal("fetch", fetchMock);
    const user = userEvent.setup();

    renderLogin();
    await user.type(screen.getByLabelText("Username"), "root");
    await user.type(screen.getByLabelText("Password"), "swaratelle");
    await user.click(screen.getByRole("button", { name: "Sign in" }));

    await waitFor(() => expect(fetchMock).toHaveBeenCalled());
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toContain("/api/auth/login");
    expect(JSON.parse(String(init.body))).toEqual({
      username: "root",
      password: "swaratelle",
    });
  });

  it("shows the server error message when login fails", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: false,
      json: async () => ({ error: "Invalid username or password." }),
    });
    vi.stubGlobal("fetch", fetchMock);
    const user = userEvent.setup();

    renderLogin();
    await user.type(screen.getByLabelText("Username"), "root");
    await user.type(screen.getByLabelText("Password"), "wrong");
    await user.click(screen.getByRole("button", { name: "Sign in" }));

    expect(
      await screen.findByText("Invalid username or password."),
    ).toBeInTheDocument();
  });
});
