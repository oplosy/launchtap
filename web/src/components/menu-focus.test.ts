import { describe, expect, it } from "vitest";
import { focusLeftMenu } from "./menu-focus";

const zone = (...members: object[]) => ({ contains: (node: object) => members.includes(node) });

describe("focusLeftMenu", () => {
  const trigger = {};
  const link = {};
  const outside = {};
  const zones = [zone(trigger), zone(link)];

  it("closes when focus moves to an element outside the trigger and the panel", () => {
    expect(focusLeftMenu(outside, zones)).toBe(true);
  });
  it("stays open while focus moves inside the panel or back to the trigger", () => {
    expect(focusLeftMenu(link, zones)).toBe(false);
    expect(focusLeftMenu(trigger, zones)).toBe(false);
  });
  it("stays open when focus goes nowhere, such as a click on panel text", () => {
    expect(focusLeftMenu(null, zones)).toBe(false);
  });
  it("ignores zones that are not mounted", () => {
    expect(focusLeftMenu(link, [null, zone(link)])).toBe(false);
    expect(focusLeftMenu(outside, [null, null])).toBe(true);
  });
});
