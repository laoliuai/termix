import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/preact";

import { Header } from "./header";

describe("Header", () => {
  beforeEach(() => {
    cleanup();
  });

  it("uses the real app icon asset for the brand mark", () => {
    const { container } = render(<Header />);

    const icon = container.querySelector(".brand-mark");
    expect(icon?.getAttribute("src")).toBe("/icons/termix.svg?v=tmx");

    const svg = readFileSync(resolve(process.cwd(), "public/icons/termix.svg"), "utf8");
    expect(svg).toContain("<svg");
    expect(svg).toContain('id="termix-tmx-logo"');
    expect(svg).toContain("TMX");
    expect(svg).not.toContain(">_");
  });

  it("keeps the browser tab icon aligned with the header app icon", () => {
    const html = readFileSync(resolve(process.cwd(), "index.html"), "utf8");

    expect(html).toContain('<link rel="icon" type="image/svg+xml" href="/icons/termix.svg?v=tmx">');
  });

  it("opens Help from the menu", () => {
    const onHelp = vi.fn();
    render(<Header onHelp={onHelp} />);

    fireEvent.click(screen.getByRole("button", { name: "menu" }));
    fireEvent.click(screen.getByRole("menuitem", { name: "Help" }));

    expect(onHelp).toHaveBeenCalledOnce();
  });
});
