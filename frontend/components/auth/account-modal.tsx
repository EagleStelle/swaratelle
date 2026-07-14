"use client";

import { useEffect, useState } from "react";
import { CircleAlert } from "lucide-react";

import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Separator } from "@/components/ui/separator";
import { useUpdateCredentials } from "@/lib/use-auth";

export function AccountModal({
  open,
  onOpenChange,
  username,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  username: string;
}) {
  const update = useUpdateCredentials();
  const [newUsername, setNewUsername] = useState(username);
  const [newPassword, setNewPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [currentPassword, setCurrentPassword] = useState("");
  const [error, setError] = useState("");

  // Reset fields each time the dialog opens so it never shows stale input.
  useEffect(() => {
    if (open) {
      setNewUsername(username);
      setNewPassword("");
      setConfirmPassword("");
      setCurrentPassword("");
      setError("");
    }
  }, [open, username]);

  function submit(event: React.FormEvent) {
    event.preventDefault();
    setError("");
    if ((newPassword || confirmPassword) && newPassword !== confirmPassword) {
      setError("New passwords do not match.");
      return;
    }
    update.mutate(
      {
        current_password: currentPassword,
        username: newUsername,
        new_password: newPassword || undefined,
      },
      {
        onSuccess: () => onOpenChange(false),
        onError: (err) =>
          setError(
            err instanceof Error ? err.message : "Could not save account.",
          ),
      },
    );
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Account</DialogTitle>
          <DialogDescription>
            Change your username and password. <br />
            Enter your current password to confirm.
          </DialogDescription>
        </DialogHeader>

        <form className="flex flex-col gap-4" onSubmit={submit}>
          <div className="flex flex-col gap-2">
            <Label htmlFor="account-username">Username</Label>
            <Input
              id="account-username"
              type="text"
              autoComplete="username"
              value={newUsername}
              onChange={(e) => setNewUsername(e.target.value)}
            />
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor="account-new-password">New password</Label>
            <Input
              id="account-new-password"
              type="password"
              autoComplete="new-password"
              value={newPassword}
              onChange={(e) => setNewPassword(e.target.value)}
            />
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor="account-confirm-password">
              Confirm new password
            </Label>
            <Input
              id="account-confirm-password"
              type="password"
              autoComplete="new-password"
              value={confirmPassword}
              onChange={(e) => setConfirmPassword(e.target.value)}
            />
          </div>

          <Separator />

          <div className="flex flex-col gap-2">
            <Label htmlFor="account-current-password">Current password</Label>
            <Input
              id="account-current-password"
              type="password"
              autoComplete="current-password"
              value={currentPassword}
              onChange={(e) => setCurrentPassword(e.target.value)}
            />
          </div>

          {error && (
            <Alert variant="destructive">
              <CircleAlert />
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          )}

          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={() => onOpenChange(false)}
            >
              Cancel
            </Button>
            <Button type="submit" disabled={update.isPending}>
              {update.isPending ? "Saving…" : "Save"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
