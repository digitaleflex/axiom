export default async function OrgPage({ params }: { params: { orgID: string } }) {
  return (
    <main className="p-8">
      <a href="/" className="text-[--color-muted-foreground] text-sm mb-4 inline-block">← Dashboard</a>
      <h1 className="text-3xl font-bold mb-6">Organization {params.orgID}</h1>
      <section className="rounded-xl border border-[--color-border] bg-[--color-card] p-6">
        <h2 className="text-xl font-semibold mb-3">Projects (M12.1)</h2>
        <p className="text-[--color-muted-foreground] text-sm">Calls /api/v1/orgs/{params.orgID}/projects — coming next.</p>
      </section>
    </main>
  );
}
