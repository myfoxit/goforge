// Sidebar navigation for the authenticated app shell. Add a NavItem here when
// you add a screen; items marked admin only show to users carrying a role.

export interface NavItem {
	href: string;
	label: string;
	icon: string; // dependency-free glyph
	admin?: boolean;
	exact?: boolean;
}

// The demo collection seeded by seed.go. Delete it in the admin and this page
// gracefully shows an empty state.
export const EXAMPLE_COLLECTION = "items";

export const nav: NavItem[] = [
	{ href: "/app", label: "Dashboard", icon: "▦", exact: true },
	{ href: "/app/data", label: "Items", icon: "☰" },
	{ href: "/app/team", label: "Team", icon: "◎" },
	{ href: "/app/users", label: "Users", icon: "⚇", admin: true },
	{ href: "/app/billing", label: "Billing", icon: "◫" },
	{ href: "/app/account", label: "Account", icon: "⚙" },
];
