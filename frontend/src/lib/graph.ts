import { scaleUtc } from "d3-scale";
export type Project = {
  id: string;
  name: string;
  slug: string;
  description: string;
  color: string;
  icon: string;
  sort_order: number;
};
export type Milestone = {
  id: string;
  project_id: string;
  title: string;
  description: string;
  milestone_type: string;
  version: string;
  planned_date: string;
  completed_date: string;
  status: string;
  owner: string;
  external_url: string;
};
export type Dependency = {
  id: string;
  prerequisite_milestone_id: string;
  dependent_milestone_id: string;
  dependency_type: string;
};
export type Graph = {
  projects: Project[];
  milestones: Milestone[];
  dependencies: Dependency[];
};
export type Conflict = {
  prerequisite_milestone_id: string;
  dependent_milestone_id: string;
  days: number;
  category: string;
};
export const utc = (s: string) => new Date(s + "T00:00:00Z");
export function timelineScale(start: string, end: string, width: number) {
  return scaleUtc()
    .domain([utc(start), utc(end)])
    .range([90, width - 90]);
}
export function chain(id: string, edges: Dependency[]) {
  const all = new Set([id]),
    up = new Map<string, string[]>(),
    down = new Map<string, string[]>();
  for (const e of edges) {
    up.set(e.dependent_milestone_id, [
      ...(up.get(e.dependent_milestone_id) || []),
      e.prerequisite_milestone_id,
    ]);
    down.set(e.prerequisite_milestone_id, [
      ...(down.get(e.prerequisite_milestone_id) || []),
      e.dependent_milestone_id,
    ]);
  }
  for (const adjacency of [up, down]) {
    const seen = new Set([id]),
      q = [id];
    while (q.length) {
      for (const next of adjacency.get(q.pop()!) || []) {
        if (!seen.has(next)) {
          seen.add(next);
          all.add(next);
          q.push(next);
        }
      }
    }
  }
  return all;
}
export function edgePath(
  x1: number,
  y1: number,
  x2: number,
  y2: number,
  offset = 0,
) {
  const direction = x2 >= x1 ? 1 : -1;
  const bend = Math.max(48, Math.abs(x2 - x1) * 0.44) + offset;
  if (y1 === y2)
    return `M ${x1 + direction * 10} ${y1} C ${x1 + direction * bend} ${y1 - 35 - offset}, ${x2 - direction * bend} ${y2 - 35 - offset}, ${x2 - direction * 12} ${y2}`;
  return `M ${x1 + direction * 10} ${y1} C ${x1 + direction * bend} ${y1}, ${x2 - direction * bend} ${y2}, ${x2 - direction * 12} ${y2}`;
}
export function localConflicts(
  milestones: Milestone[],
  edges: Dependency[],
): Conflict[] {
  const m = new Map(milestones.map((x) => [x.id, x]));
  return edges
    .filter((e) => e.dependency_type === "requires")
    .flatMap((e) => {
      const a = m.get(e.prerequisite_milestone_id),
        b = m.get(e.dependent_milestone_id);
      if (
        !a ||
        !b ||
        a.status === "cancelled" ||
        b.status === "cancelled" ||
        b.status === "completed"
      )
        return [];
      const days = Math.round(
        (utc(
          a.status === "completed" && a.completed_date
            ? a.completed_date
            : a.planned_date,
        ).getTime() -
          utc(b.planned_date).getTime()) /
          86400000,
      );
      return days > 0
        ? [
            {
              prerequisite_milestone_id: a.id,
              dependent_milestone_id: b.id,
              days,
              category: "schedule_conflict",
            },
          ]
        : [];
    });
}
