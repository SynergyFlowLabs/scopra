import "server-only";
import { auth } from "../auth";

export type DashboardIdentity = { githubSubject: string };
export type RouteIdentityResult =
  | { identity: DashboardIdentity; response?: never }
  | { identity?: never; response: Response };

export async function requireDashboardIdentity(): Promise<RouteIdentityResult> {
  if (!process.env.AUTH_SECRET || !process.env.AUTH_GITHUB_ID || !process.env.AUTH_GITHUB_SECRET) {
    return { response: Response.json({ error: { code: "auth_not_configured", message: "Dashboard sign-in is not configured." } }, { status: 503, headers: { "Cache-Control": "no-store" } }) };
  }
  try {
    const session = await auth();
    const githubSubject = session?.user?.id;
    if (typeof githubSubject !== "string" || !/^[0-9]{1,20}$/.test(githubSubject)) {
      return { response: Response.json({ error: { code: "authentication_required", message: "Sign in is required." } }, { status: 401, headers: { "Cache-Control": "no-store" } }) };
    }
    return { identity: { githubSubject } };
  } catch {
    return { response: Response.json({ error: { code: "authentication_unavailable", message: "Dashboard sign-in is unavailable." } }, { status: 503, headers: { "Cache-Control": "no-store" } }) };
  }
}
