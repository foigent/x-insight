export interface DeclaredEdge { from: string; to: string }

// Only API-declared edges whose two endpoints are visible participate. The DAG
// summary uses dynamic programming, avoiding enumeration of every branch path.
export function declaredPath<N extends { event_id: string }>(nodes: N[], edges: DeclaredEdge[]): N[] {
  const byID = new Map(nodes.map(n => [n.event_id, n]));
  const children = new Map(nodes.map(n => [n.event_id, new Set<string>()]));
  const incoming = new Map(nodes.map(n => [n.event_id, 0]));
  for (const edge of edges) if (byID.has(edge.from) && byID.has(edge.to) && !children.get(edge.from)!.has(edge.to)) {
    children.get(edge.from)!.add(edge.to); incoming.set(edge.to, incoming.get(edge.to)! + 1);
  }
  const queue = nodes.filter(n => incoming.get(n.event_id) === 0).map(n => n.event_id).sort();
  const paths = new Map(nodes.map(n => [n.event_id, [n.event_id]]));
  const better = (a: string[], b: string[]) => a.length > b.length || a.length === b.length && a.join("\0") < b.join("\0");
  let best: string[] = [], visited = 0;
  for (let i = 0; i < queue.length; i++) {
    const id = queue[i], path = paths.get(id)!; visited++;
    if (better(path, best)) best = path;
    for (const child of [...children.get(id)!].sort()) {
      const next = [...path, child]; if (better(next, paths.get(child)!)) paths.set(child, next);
      incoming.set(child, incoming.get(child)! - 1);
      if (incoming.get(child) === 0) queue.push(child);
    }
  }
  // A cyclic graph has no whole-graph DAG path summary; full visible nodes and
  // source edges remain available in the graph without inventing a break.
  return visited === nodes.length ? best.map(id => byID.get(id)!) : [];
}

export function graphPositions(nodes: { event_id: string }[], edges: DeclaredEdge[]) {
  const layer = new Map(nodes.map(node => [node.event_id, 0]));
  for (let pass = 0; pass < nodes.length; pass++) {
    let changed = false;
    for (const edge of edges) {
      const from = layer.get(edge.from), to = layer.get(edge.to);
      if (from === undefined || to === undefined) continue;
      const next = Math.min(nodes.length - 1, from + 1);
      if (next > to) { layer.set(edge.to, next); changed = true; }
    }
    if (!changed) break;
  }
  const rows = new Map<number, number>();
  return nodes.map((node, index) => {
    const depth = layer.get(node.event_id) ?? 0, row = rows.get(depth) ?? 0; rows.set(depth, row + 1);
    return { id: node.event_id, index, x: 30 + depth * 190, y: 34 + row * 94 };
  });
}
