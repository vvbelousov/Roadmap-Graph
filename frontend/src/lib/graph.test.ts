import { describe, it, expect } from "vitest";
import {
  chain,
  timelineScale,
  utc,
  edgePath,
  localConflicts,
  type Milestone,
  type Dependency,
} from "./graph";
const m = (
  id: string,
  date = "2027-01-10",
  status = "planned",
  completed_date = "",
): Milestone => ({
  id,
  project_id: "p",
  title: id,
  description: "",
  milestone_type: "release",
  version: "",
  planned_date: date,
  completed_date,
  status,
  owner: "",
  external_url: "",
});
const e = (a: string, b: string, type = "requires"): Dependency => ({
  id: a + b,
  prerequisite_milestone_id: a,
  dependent_milestone_id: b,
  dependency_type: type,
});
describe("timeline and dependency model", () => {
  it("keeps actual calendar dates in linear UTC positions at each scale", () => {
    for (const width of [500, 1200, 6000]) {
      const scale = timelineScale("2027-01-01", "2027-01-31", width);
      expect(scale(utc("2027-01-16"))).toBe((90 + width - 90) / 2);
      expect(utc("2027-01-01").toISOString()).toBe("2027-01-01T00:00:00.000Z");
    }
  });
  it("traverses ancestors and descendants without pulling in siblings", () => {
    expect(
      [
        ...chain("b", [
          e("a", "b"),
          e("b", "c"),
          e("a", "sibling"),
          e("c", "d"),
        ]),
      ].sort(),
    ).toEqual(["a", "b", "c", "d"]);
  });
  it("terminates for informational cycles", () => {
    expect(
      chain("a", [e("a", "b", "related_to"), e("b", "a", "related_to")]).size,
    ).toBe(2);
  });
  it("routes same-lane dependencies away from markers", () => {
    expect(edgePath(10, 50, 100, 50)).toContain("15");
    expect(edgePath(10, 50, 100, 100)).toContain("C");
  });
  it("only hard dependencies create conflicts and uses actual completed availability", () => {
    const milestones = [
      m("a", "2027-01-20"),
      m("b"),
      m("c", "2027-01-20", "completed", "2027-01-05"),
    ];
    expect(
      localConflicts(milestones, [
        e("a", "b"),
        e("c", "b"),
        e("a", "c", "related_to"),
      ]),
    ).toEqual([
      {
        prerequisite_milestone_id: "a",
        dependent_milestone_id: "b",
        days: 10,
        category: "schedule_conflict",
      },
    ]);
    expect(
      localConflicts(
        [m("a", "2027-01-20"), m("b", "2027-01-10", "completed", "2027-01-10")],
        [e("a", "b")],
      ),
    ).toEqual([]);
  });
  it("processes target 30/300/500 model with measured timings", () => {
    const milestones = Array.from({ length: 300 }, (_, i) => ({
      ...m("m" + i, `2027-${String(1 + (i % 12)).padStart(2, "0")}-10`),
      project_id: "p" + (i % 30),
    }));
    const edges = Array.from({ length: 500 }, (_, i) =>
      e(
        "m" + (i < 299 ? i : i % 298),
        "m" + (i < 299 ? i + 1 : Math.min(299, (i % 298) + 2)),
      ),
    );
    const started = performance.now();
    for (let n = 0; n < 100; n++) {
      chain("m150", edges);
      localConflicts(milestones, edges);
      const scale = timelineScale("2027-01-01", "2028-01-01", 2500);
      milestones.forEach((m) => scale(utc(m.planned_date)));
    }
    console.log(
      `Graph model 30 projects / 300 milestones / 500 dependencies: 100 chain+conflict+position passes ${(performance.now() - started).toFixed(1)}ms`,
    );
    expect(chain("m150", edges).size).toBeGreaterThan(150);
  });
});
