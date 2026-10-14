// Only fixed diagnostic categories and counts leave the host. Native error
// strings, paths, event bodies and credentials never enter this snapshot.
function category(code) {
  if (code === 'body_item_limit') return 'body_limit';
  if (code === 'outbox_capacity_exceeded' || code === 'body_queue_full') return 'outbox_full';
  if (code.startsWith('body_')) return 'body_unavailable';
  if (code.startsWith('future_')) return 'future_scope_unconfirmed';
  if (code.includes('renew')) return 'renewal_unconfirmed';
  if (code.startsWith('ur_') || code.startsWith('managed_provider_') ||
    code.startsWith('managed_ur_') || code === 'managed_existing_provider_conflict') return 'ur_relation_unconfirmed';
  if (code.includes('project') || code === 'scope_changed') return 'source_mapping_unconfirmed';
  if (code === 'receiver_unavailable' || code === 'work_event_config_unavailable') return 'receiver_unavailable';
  if (code.startsWith('metadata_') || code === 'transport_unavailable' || code === 'invalid_ack') return 'metadata_unavailable';
  return 'observation_unavailable';
}
export function installStatusSnapshot(metadata, bodyPending, gaps) {
  const count = value => Number.isSafeInteger(value) && value >= 0;
  if (!metadata || !count(metadata.pending) || !count(metadata.blocked) || !count(bodyPending) || !Array.isArray(gaps)) {
    throw new TypeError('invalid installation counters');
  }
  const grouped = new Map();
  for (const item of gaps) {
    if (typeof item?.code !== 'string' || !count(item.occurrences) || item.occurrences === 0) throw new TypeError('invalid installation gap');
    const key = category(item.code);
    const total = (grouped.get(key) ?? 0) + item.occurrences;
    if (!Number.isSafeInteger(total)) throw new TypeError('installation counter overflow');
    grouped.set(key, total);
  }
  return {metadata_pending:metadata.pending, metadata_blocked:metadata.blocked, body_pending:bodyPending,
    gaps:[...grouped].sort(([a],[b])=>a.localeCompare(b)).map(([code,occurrences])=>({code,occurrences}))};
}
