import { AdminOnly } from "@/components/common/admin-only";
import { TicketProjectsView } from "@/components/ticket-projects/ticket-projects-view";

export default function TicketProjectsPage(): React.ReactElement {
  return (
    <AdminOnly>
      <TicketProjectsView />
    </AdminOnly>
  );
}
