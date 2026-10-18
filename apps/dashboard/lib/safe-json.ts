import "server-only";

/** Read a bounded JSON object and reject duplicate keys before last-key-wins parsing. */
export async function readBoundedJSONObject(request: Request, maxBytes: number): Promise<Record<string, unknown> | null> {
  const contentLength = request.headers.get("content-length");
  if (contentLength && (!/^\d+$/.test(contentLength) || Number(contentLength) > maxBytes)) {
    await request.body?.cancel().catch(() => undefined);
    return null;
  }
  if (!request.body || !Number.isSafeInteger(maxBytes) || maxBytes < 1) return null;

  const reader = request.body.getReader();
  const chunks: Uint8Array[] = [];
  let size = 0;
  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      size += value.byteLength;
      if (size > maxBytes) {
        await reader.cancel().catch(() => undefined);
        return null;
      }
      chunks.push(value);
    }
  } catch {
    return null;
  }
  const bytes = new Uint8Array(size);
  let offset = 0;
  for (const chunk of chunks) {
    bytes.set(chunk, offset);
    offset += chunk.byteLength;
  }
  try {
    const text = new TextDecoder("utf-8", { fatal: true }).decode(bytes);
    assertNoDuplicateJSONKeys(text);
    const value: unknown = JSON.parse(text);
    if (value === null || typeof value !== "object" || Array.isArray(value)) return null;
    return value as Record<string, unknown>;
  } catch {
    return null;
  }
}

function assertNoDuplicateJSONKeys(text: string): void {
  let position = 0;
  const whitespace = () => {
    while (position < text.length && (text[position] === " " || text[position] === "\n" || text[position] === "\r" || text[position] === "\t")) position += 1;
  };
  const stringToken = (): string => {
    if (text[position] !== '"') throw new Error("expected JSON string");
    const start = position;
    position += 1;
    while (position < text.length) {
      const character = text[position];
      if (character === '"') {
        position += 1;
        const parsed: unknown = JSON.parse(text.slice(start, position));
        if (typeof parsed !== "string") throw new Error("invalid JSON string");
        return parsed;
      }
      if (character === "\\") {
        position += 1;
        if (position >= text.length) throw new Error("unterminated JSON escape");
      }
      position += 1;
    }
    throw new Error("unterminated JSON string");
  };
  const value = (depth: number): void => {
    if (depth > 64) throw new Error("JSON nesting exceeds the limit");
    whitespace();
    const character = text[position];
    if (character === "{") {
      position += 1;
      whitespace();
      if (text[position] === "}") { position += 1; return; }
      const keys = new Set<string>();
      while (position < text.length) {
        whitespace();
        const key = stringToken();
        if (keys.has(key)) throw new Error("duplicate JSON object key");
        keys.add(key);
        whitespace();
        if (text[position] !== ":") throw new Error("missing JSON object separator");
        position += 1;
        value(depth + 1);
        whitespace();
        if (text[position] === "}") { position += 1; return; }
        if (text[position] !== ",") throw new Error("invalid JSON object");
        position += 1;
      }
      throw new Error("unterminated JSON object");
    }
    if (character === "[") {
      position += 1;
      whitespace();
      if (text[position] === "]") { position += 1; return; }
      while (position < text.length) {
        value(depth + 1);
        whitespace();
        if (text[position] === "]") { position += 1; return; }
        if (text[position] !== ",") throw new Error("invalid JSON array");
        position += 1;
      }
      throw new Error("unterminated JSON array");
    }
    if (character === '"') {
      stringToken();
      return;
    }
    const start = position;
    while (position < text.length && ![",", "]", "}", " ", "\n", "\r", "\t"].includes(text[position])) position += 1;
    if (position === start) throw new Error("invalid JSON value");
  };

  value(0);
  whitespace();
  if (position !== text.length) throw new Error("trailing JSON data");
}
