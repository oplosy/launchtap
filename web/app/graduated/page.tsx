import { permanentRedirect } from "next/navigation";

export default function GraduatedPage() {
  permanentRedirect("/explore?view=graduated" as never);
}
