import { listMachines } from "../../../lib/control-plane";
import { controlPlaneErrorResponse } from "../../../lib/route-error";
import { requireDashboardIdentity } from "../../../lib/route-auth";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function GET(): Promise<Response> {
  const result = await requireDashboardIdentity();
  if (!result.identity) return result.response;
  try {
    const machines = await listMachines(result.identity.githubSubject);
    return Response.json({ machines }, { headers: { "Cache-Control": "no-store, private" } });
  } catch (error) {
    return controlPlaneErrorResponse(error);
  }
}
