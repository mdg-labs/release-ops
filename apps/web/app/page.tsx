import { Button } from "@/components/ui/button";

export default function HomePage() {
  return (
    <main className="flex min-h-screen flex-col items-center justify-center gap-4 p-8">
      <h1 className="text-2xl font-semibold">Release Ops</h1>
      <p className="text-muted-foreground">
        Self-hosted release monitor scaffold
      </p>
      <Button type="button">Get started</Button>
    </main>
  );
}
