import { GoogleAuth } from "google-auth-library";
const auth = new GoogleAuth();
export async function cloudHeaders(
  audience: string,
): Promise<Record<string, string>> {
  if (process.env.GCP_AUTH !== "true") return {};
  const client = await auth.getIdTokenClient(audience);
  const headers = await client.getRequestHeaders();
  return { "X-Serverless-Authorization": headers.get("authorization") ?? "" };
}
