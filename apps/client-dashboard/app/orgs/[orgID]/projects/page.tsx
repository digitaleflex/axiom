export default async function ProjectsPage({ params }: { params: { orgID: string } }) {
  return (
    <main className="p-8">
      <a href={`/orgs/${params.orgID}`} className="text-[--color-muted-foreground] text-sm mb-4 inline-block">← Organization</a>
      <h1 className="text-3xl font-bold mb-6">Projects</h1>
      <section className="rounded-xl border border-[--color-border] bg-[--color-card] p-6">
        <p className="text-[--color-muted-foreground] text-sm mb-4">Fetches /api/v1/orgs/{params.orgID}/projects (M12.1).</p>
        <ul className="divide-y divide-[--color-border]">
          <li className="py-3"><span className="font-medium">Web App</span> <span className="text-xs bg-[--color-muted] px-2 py-0.5 rounded-full ml-2">staging</span></li>
          <li className="py-3"><span className="font-medium">API</span> <span className="text-xs bg-[--color-muted] px-2 py-0.5 rounded-full ml-2">production</span></li>
        </ul>
      </section>
      <section className="rounded-xl border border-[--color-border] bg-[--color-card] p-6 mt-6">
        <h2 className="text-lg font-semibold mb-2">Plan & Quota (M12.4)</h2>
        <p className="text-[--color-muted-foreground] text-sm">Quota enforced by Engine /plan check. Billing layer pending.</p>
      </section>
    </main>
  );
}
