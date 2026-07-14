// Auth calls for the web-UI login. The UI is served same-origin by the Go
// service, so requests carry the session cookie automatically. This login only
// gates the UI; external clients use the SWARATELLE_API_TOKEN bearer instead.
import { API_BASE } from "@/lib/api";

export interface AuthSession {
  authenticated: boolean;
  username: string;
}

export interface LoginPayload {
  username: string;
  password: string;
}

export interface CredentialsPayload {
  current_password: string;
  username: string;
  new_password?: string;
}

async function readError(res: Response, fallback: string): Promise<string> {
  try {
    const data = (await res.json()) as { error?: string };
    return data.error?.trim() || fallback;
  } catch {
    return fallback;
  }
}

export async function getAuthSession(): Promise<AuthSession> {
  const res = await fetch(`${API_BASE}/api/auth/session`, {
    credentials: "same-origin",
  });
  if (!res.ok) {
    throw new Error(await readError(res, "Could not check login."));
  }
  const data = (await res.json()) as Partial<AuthSession> | null;
  return {
    authenticated: Boolean(data?.authenticated),
    username: data?.username ?? "",
  };
}

export async function login(payload: LoginPayload): Promise<AuthSession> {
  const res = await fetch(`${API_BASE}/api/auth/login`, {
    method: "POST",
    credentials: "same-origin",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
  if (!res.ok) {
    throw new Error(await readError(res, "Could not sign in."));
  }
  return (await res.json()) as AuthSession;
}

export async function logout(): Promise<AuthSession> {
  const res = await fetch(`${API_BASE}/api/auth/logout`, {
    method: "POST",
    credentials: "same-origin",
  });
  if (!res.ok) {
    throw new Error(await readError(res, "Could not sign out."));
  }
  return (await res.json()) as AuthSession;
}

export async function updateCredentials(
  payload: CredentialsPayload,
): Promise<AuthSession> {
  const res = await fetch(`${API_BASE}/api/auth/credentials`, {
    method: "POST",
    credentials: "same-origin",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
  if (!res.ok) {
    throw new Error(await readError(res, "Could not save account."));
  }
  return (await res.json()) as AuthSession;
}
