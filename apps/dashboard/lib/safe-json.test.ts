import assert from "node:assert/strict";
import test from "node:test";
import { readBoundedJSONObject } from "./safe-json";

function request(body: BodyInit, headers: Record<string, string> = {}): Request {
  return new Request("https://dashboard.example/api/test", {
    method: "POST",
    headers: { "content-type": "application/json", ...headers },
    body,
  });
}

test("reads bounded JSON objects with nested values", async () => {
  const body = await readBoundedJSONObject(request(JSON.stringify({ pairingCode: "abc", metadata: { note: "a,b:c" } })), 1024);
  assert.deepEqual(body, { pairingCode: "abc", metadata: { note: "a,b:c" } });
});

test("rejects duplicate decoded keys at every object depth", async () => {
  for (const encoded of [
    '{"pairingCode":"first","pairingCode":"second"}',
    '{"pairingCode":"first","pairing\\u0043ode":"second"}',
    '{"pairingCode":"abc","nested":{"value":1,"value":2}}',
  ]) {
    assert.equal(await readBoundedJSONObject(request(encoded), 1024), null, encoded);
  }
});

test("rejects malformed, non-object, invalid UTF-8, and too-large bodies", async () => {
  for (const encoded of ["{", "[]", "null", "false", "\"text\""]) {
    assert.equal(await readBoundedJSONObject(request(encoded), 1024), null, encoded);
  }
  assert.equal(await readBoundedJSONObject(request(new Uint8Array([0xff])), 1024), null);
  assert.equal(await readBoundedJSONObject(request(JSON.stringify({ value: "x".repeat(1100) })), 1024), null);
  assert.equal(await readBoundedJSONObject(request("{}", { "content-length": "2049" }), 1024), null);
});
