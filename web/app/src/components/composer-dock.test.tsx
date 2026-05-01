import { describe, it, expect } from "vitest";
import { render, cleanup } from "@testing-library/preact";

import { ComposerDock } from "./composer-dock";

describe("ComposerDock", () => {
  it("does not render children when closed", () => {
    const { container } = render(<ComposerDock open={false}><span data-testid="kid">kid</span></ComposerDock>);
    expect(container.querySelector("[data-testid='kid']")).toBeNull();
    expect(container.querySelector(".composer-dock")?.classList.contains("is-open")).toBe(false);
    cleanup();
  });

  it("renders children and toggles is-open class when open", () => {
    const { container } = render(<ComposerDock open={true}><span data-testid="kid">kid</span></ComposerDock>);
    expect(container.querySelector("[data-testid='kid']")).toBeTruthy();
    expect(container.querySelector(".composer-dock")?.classList.contains("is-open")).toBe(true);
    cleanup();
  });
});
