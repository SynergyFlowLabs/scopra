import { claimPairing } from "../../../../lib/control-plane";
import { controlPlaneErrorResponse } from "../../../../lib/route-error";
import { requireDashboardIdentity } from "../../../../lib/route-auth";
import { isSameOriginRequest } from "../../../../lib/same-origin";
import { readBoundedJSONObject } from "../../../../lib/safe-json";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

const CONTROL_CHARACTERS = /[\u0000-\u001f\u007f-\u009f\u2028\u2029]/u;

export async function POST(request: Request): Promise<Response> {
  if (!isSameOriginRequest(request)) return error(403, "same_origin_required", "Request origin was not accepted.");
  if (!/^application\/json(?:\s*;\s*charset=utf-8)?$/i.test(request.headers.get("content-type") ?? "")) {
    return error(415, "unsupported_media_type", "JSON is required.");
  }
  const identity = await requireDashboardIdentity();
  if (!identity.identity) return identity.response;
  const body = await readBoundedJSONObject(request, 8192);
  if (!body || Object.keys(body).length !== 3 ||
      typeof body.pairingCode !== "string" || !/^[A-Za-z0-9_-]{22}$/.test(body.pairingCode) ||
      typeof body.fingerprint !== "string" || !/^[0-9a-f]{24}$/.test(body.fingerprint) ||
      typeof body.workspacePath !== "string" || !body.workspacePath.startsWith("/") ||
      body.workspacePath.length > 4095 || CONTROL_CHARACTERS.test(body.workspacePath)) {
    return error(400, "invalid_request", "Pairing confirmation details are invalid.");
  }
  try {
    const result = await claimPairing(identity.identity.githubSubject, {
      pairingCode: body.pairingCode,
      fingerprint: body.fingerprint,
      workspacePath: body.workspacePath,
    });
    return Response.json(result, { headers: { "Cache-Control": "no-store, private" } });
  } catch (cause) {
    return controlPlaneErrorResponse(cause);
  }
}

function error(status: number, code: string, message: string): Response {
  return Response.json({ error: { code, message } }, { status, headers: { "Cache-Control": "no-store" } });
}
