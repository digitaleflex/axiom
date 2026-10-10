import { auth } from "@/lib/auth";
import { redirect } from "next/navigation";

export default async function RootPage() {
  const session = await auth.api.getSession({ headers: await auth.getHeaders() });
  if (!session?.user) redirect("/login");

  // M12.3 — calls Engine /api/v1/orgs (M12.1) to list tenant orgs.
  const orgsRes = await fetch(
    `${process.env.NEXT_PUBLIC_ENGINE_URL}/api/v1/orgs`,
    { headers: { Authorization: `Bearer ${session.session.token}` }, cache: "no-store" }
  );
  const orgs = orgsRes.ok ? await orgsRes.json() : { items: [] };

  return (
    <main className="max-w-3xl mx-auto p-8">
      <h1 className="text-3xl font-bold mb-6">Axiom — Dashboard</h1>
      <section className="rounded-xl border border-[--color-border] bg-[--color-card] p-6 mb-6">
        <h2 className="text-xl font-semibold mb-3">Organizations (M12.1)</h2>
        <ul className="space-y-2">
          {(orgs.items || []).map((o: { id: string; name: string }) => (
            <li key={o.id} className="p-3 rounded-lg bg-[--color-muted] hover:bg-[--color-border] transition">
              <a href={`/orgs/${o.id}`} className="font-medium">{o.name}</a>
            </li>
          ))}
        </ul>
      </section>
      <p className="text-[--color-muted-foreground] text-sm">Design spec: docs/design/ • Stack: docs/tech-stack-2024-2025.md</p>
    </main>
  );
}
