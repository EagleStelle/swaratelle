"use client";

import { useState } from "react";
import { CircleAlert } from "lucide-react";

import { LogoMark } from "@/components/logo-mark";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useLogin } from "@/lib/use-auth";

export function Login() {
  const loginMutation = useLogin();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");

  function submit(event: React.FormEvent) {
    event.preventDefault();
    setError("");
    loginMutation.mutate(
      { username, password },
      {
        onSuccess: () => setPassword(""),
        onError: (err) =>
          setError(err instanceof Error ? err.message : "Could not sign in."),
      },
    );
  }

  return (
    <main className="flex min-h-screen items-center justify-center bg-slate-100 p-4 dark:bg-background">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <div className="flex items-center gap-3">
            <LogoMark className="size-9" />
            <CardTitle className="text-xl">Swaratelle</CardTitle>
          </div>
        </CardHeader>
        <CardContent>
          <form className="flex flex-col gap-4" onSubmit={submit} aria-label="Sign in">
            <div className="flex flex-col gap-2">
              <Label htmlFor="login-username">Username</Label>
              <Input
                id="login-username"
                type="text"
                autoComplete="username"
                autoFocus
                value={username}
                onChange={(e) => setUsername(e.target.value)}
              />
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="login-password">Password</Label>
              <Input
                id="login-password"
                type="password"
                autoComplete="current-password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
              />
            </div>

            {error && (
              <Alert variant="destructive">
                <CircleAlert />
                <AlertDescription>{error}</AlertDescription>
              </Alert>
            )}

            <Button type="submit" disabled={loginMutation.isPending}>
              {loginMutation.isPending ? "Signing in…" : "Sign in"}
            </Button>
          </form>
        </CardContent>
      </Card>
    </main>
  );
}
