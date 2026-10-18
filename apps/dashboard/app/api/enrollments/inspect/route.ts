import { inspectPairing } from "../../../../lib/control-plane";
import { controlPlaneErrorResponse } from "../../../../lib/route-error";
import { requireDashboardIdentity } from "../../../../lib/route-auth";
import { isSameOriginRequest } from "../../../../lib/same-origin";
import { readBoundedJSONObject } from "../../../../lib/safe-json";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function POST(request: Request): Promise<Response> {
  if (!isSameOriginRequest(request)) return error(403, "same_origin_required", "Request origin was not accepted.");
  if (!isJSON(request)) return error(415, "unsupported_media_type", "JSON is required.");
  const identity = await requireDashboardIdentity();
  if (!identity.identity) return identity.response;
  const body = await readBoundedJSONObject(request, 2048);
  if (!body || Object.keys(body).length !== 1 || !Object.hasOwn(body, "pairingCode") ||
      typeof body.pairingCode !== "string" || !/^[A-Za-z0-9_-]{22}$/.test(body.pairingCode)) {
    return error(400, "invalid_request", "Enter the pairing code exactly as shown by Scopra.");
  }
  try {
    const details = await inspectPairing(identity.identity.githubSubject, body.pairingCode);
    return Response.json(details, { headers: { "Cache-Control": "no-store, private" } });
  } catch (cause) {
    return controlPlaneErrorResponse(cause);
  }
}

function isJSON(request: Request): boolean {
  return /^application\/json(?:\s*;\s*charset=utf-8)?$/i.test(request.headers.get("content-type") ?? "");
}

function error(status: number, code: string, message: string): Response {
  return Response.json({ error: { code, message } }, { status, headers: { "Cache-Control": "no-store" } });
}
