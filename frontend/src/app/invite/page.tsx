import { InvitationAcceptance } from "@/components/invitation-acceptance";
export const metadata = { title: "Accept invitation", referrer: "same-origin" };
export default function InvitePage() {
  return <main className="mx-auto max-w-lg space-y-5 p-8"><h1 className="text-2xl font-semibold">Join your team</h1><p>Sign in using the verified email address that received this invitation. Your organization manages account registration through its sign-in provider.</p><InvitationAcceptance /></main>;
}
