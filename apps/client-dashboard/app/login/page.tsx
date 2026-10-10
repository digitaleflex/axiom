export default function LoginPage() {
  return (
    <main className="flex min-h-screen items-center justify-center">
      <section className="rounded-xl border border-[--color-border] bg-[--color-card] p-8 w-full max-w-md shadow-xl">
        <h1 className="text-2xl font-bold mb-2">Axiom</h1>
        <p className="text-[--color-muted-foreground] mb-6">Client dashboard — M12.3</p>
        <form action="#" className="space-y-4">
          <input placeholder="Email" className="w-full rounded-lg border border-[--color-border] bg-[--color-background] px-4 py-3" />
          <button className="w-full rounded-lg bg-[--color-primary] text-[--color-primary-foreground] px-4 py-3 font-semibold hover:brightness-110 transition">Continue</button>
        </form>
        <p className="text-xs text-[--color-muted-foreground] mt-6">Auth via Better Auth + Engine session (#146).</p>
      </section>
    </main>
  );
}
