import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { Button } from "@/components/ui/button";

describe("Button", () => {
  it("renders its children", () => {
    render(<Button>Queue downloads</Button>);
    expect(
      screen.getByRole("button", { name: "Queue downloads" })
    ).toBeInTheDocument();
  });

  it("can be disabled", () => {
    render(<Button disabled>Working...</Button>);
    expect(screen.getByRole("button")).toBeDisabled();
  });
});
