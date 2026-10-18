import { ControlPlaneError } from "./control-plane";

export function controlPlaneErrorResponse(error: unknown): Response {
  if (error instanceof ControlPlaneError) {
    return Response.json({ error: { code: error.code, message: error.message } }, {
      status: error.status,
      headers: { "Cache-Control": "no-store" },
    });
  }
  return Response.json({ error: { code: "upstream_unavailable", message: "Control plane is unavailable." } }, {
    status: 503,
    headers: { "Cache-Control": "no-store" },
  });
}
