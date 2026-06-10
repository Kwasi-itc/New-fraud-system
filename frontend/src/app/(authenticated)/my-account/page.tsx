import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

export default function MyAccountPage() {
  return (
    <Card>
      <CardHeader>
        <CardTitle>My Account</CardTitle>
        <CardDescription>
          Personal profile, analyst permissions, and session controls.
        </CardDescription>
      </CardHeader>
      <CardContent>
        <div className="grid gap-4 md:grid-cols-2">
          <div className="rounded-[24px] bg-muted p-5">
            <p className="text-sm font-medium">Role</p>
            <p className="mt-2 text-sm leading-7 text-muted-foreground">
              Senior fraud analyst with review, export, and escalation privileges.
            </p>
          </div>
          <div className="rounded-[24px] bg-muted p-5">
            <p className="text-sm font-medium">Security</p>
            <p className="mt-2 text-sm leading-7 text-muted-foreground">
              Session activity, MFA status, and device approvals live here.
            </p>
          </div>
        </div>
      </CardContent>
    </Card>
  );
}
