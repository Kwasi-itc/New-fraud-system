import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
export default async function LoginPage({ searchParams }: { searchParams: Promise<{ error?: string }> }) {
  const { error } = await searchParams;
  return <Card className="w-full rounded-3xl bg-white"><CardHeader><CardTitle>Sign in</CardTitle><CardDescription>Use your organization account to access ITC Fraud System.</CardDescription></CardHeader>
    <CardContent className="space-y-4">{error && <p role="alert" className="text-sm text-red-700">Sign-in could not complete. Your account may need platform access from an administrator. Please try again or contact your administrator.</p>}
      <form action="/api/auth/start" method="post"><Button type="submit" className="w-full">Continue to sign in</Button></form>
    </CardContent></Card>;
}
