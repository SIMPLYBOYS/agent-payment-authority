// Independent display-adapter contract probe. Does NOT run the Go implementation.
// Both consumers read committed, fixed expected vectors; no regeneration mode.
import { readFileSync } from 'node:fs';
import { createHash } from 'node:crypto';
import { fileURLToPath } from 'node:url';
import { resolve, dirname } from 'node:path';
import assert from 'node:assert/strict';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const read = name => JSON.parse(readFileSync(resolve(root, 'testdata/contracts', name), 'utf8'));
const domain = 'agent-payment-authority/m1-fixture/v2/verifier-not-deployed';
export function canonical(value) {
  if (value === null || typeof value === 'boolean') return JSON.stringify(value);
  if (typeof value === 'number') {
    if (!Number.isSafeInteger(value) || Object.is(value, -0)) throw Error('not a safe integer');
    return String(value);
  }
  if (typeof value === 'string') {
    if (!value.isWellFormed() || value.includes('\ufffd')) throw Error('invalid string');
    return JSON.stringify(value).replace(/[<>&\u2028\u2029]/g, c => '\\u' + c.charCodeAt(0).toString(16).padStart(4, '0'));
  }
  if (Array.isArray(value)) return '[' + value.map(canonical).join(',') + ']';
  if (typeof value === 'object') return '{' + Object.keys(value)
    .sort((a, b) => Buffer.compare(Buffer.from(a), Buffer.from(b)))
    .map(k => canonical(k) + ':' + canonical(value[k])).join(',') + '}';
  throw Error('unsupported type');
}
export function digest(suffix, value) {
  return 'sha256:' + createHash('sha256').update(domain + '/' + suffix + '\0').update(canonical(value)).digest('hex');
}

// This adapter consumes already validated host data, not untrusted JSON. JS
// JSON.parse loses duplicate keys; strict ingress remains the Go Decode API.
if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const p = read('purchase-input.json'), v = read('golden.json');
  assert.equal(digest('resource', {http_method:p.intent.http_method, resource_url:p.intent.resource_url}), v.resource_hash);
  assert.equal(digest('requirement', p.quote), v.payment_requirement_hash);
  assert.equal(p.intent.resource_hash, v.resource_hash);
  assert.equal(p.intent.payment_requirement_hash, v.payment_requirement_hash);
  assert.equal(canonical(p.intent), v.canonical_intent);
  assert.equal(digest('intent', p.intent), v.intent_hash);
  for (const vector of v.canonical_cases) assert.equal(canonical(vector.input), vector.canonical);
  // Event chain: each prev_event_hash is the previous event's hash, the first
  // is the task genesis; an event's own hash covers its prev_event_hash.
  let prev = digest('event-genesis', {task_id: 'task-a'});
  assert.equal(prev, v.event_genesis_task_a);
  read('events.json').forEach((event, i) => {
    assert.equal(event.prev_event_hash, prev);
    prev = digest('event', event);
    assert.equal(prev, v.event_hashes[i]);
  });
  console.log('PASS: display-adapter probe matches committed Go fixture vectors');
}
