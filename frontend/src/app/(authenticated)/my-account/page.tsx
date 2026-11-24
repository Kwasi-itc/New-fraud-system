import { redirect } from "next/navigation";
import { accountProfile, manageSignInURL, platformSession } from "@/lib/server/auth";
import { AccountProfilePage } from "@/components/account/account-profile";

export default async function MyAccountPage() {
  const current = await platformSession();
  if (!current) redirect("/login");
  const profile = await accountProfile();
  if (!profile) redirect("/login");
  return <AccountProfilePage profile={profile} capabilities={current.session.capabilities} canManageSignIn={!!manageSignInURL()} />;
}
