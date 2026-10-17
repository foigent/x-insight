import { expect, it } from "vitest";
import { declaredPath, graphPositions } from "./declaredGraph";

it("handles a branching DAG without enumerating its exponentially many paths and keeps every node reachable", () => {
  const nodes = Array.from({ length: 60 }, (_, i) => ({ event_id: String(i).padStart(3, "0") }));
  const edges: { from: string; to: string }[] = [];
  for (let layer = 0; layer < 29; layer++) for (let left = 0; left < 2; left++) for (let right = 0; right < 2; right++) edges.push({ from: nodes[layer * 2 + left].event_id, to: nodes[(layer + 1) * 2 + right].event_id });
  const path = declaredPath(nodes, edges);
  expect(path).toHaveLength(30);
  expect(path.every((n, i) => n === nodes[i * 2])).toBe(true);
  const positions = graphPositions(nodes, edges);
  expect(new Set(positions.map(p => `${p.x}:${p.y}`)).size).toBe(nodes.length);
  expect(positions.map(p => p.id)).toEqual(nodes.map(n => n.event_id));
});
it("does not infer hidden endpoints or a causal path through a cycle", () => {
  const nodes = [{ event_id: "a" }, { event_id: "b" }, { event_id: "c" }];
  expect(declaredPath(nodes, [{ from: "a", to: "hidden" }, { from: "hidden", to: "c" }])).toHaveLength(1);
  const edges = [{ from: "a", to: "b" }, { from: "b", to: "a" }];
  expect(declaredPath(nodes, edges)).toEqual([]);
  expect(graphPositions(nodes, edges)).toHaveLength(3);
});
