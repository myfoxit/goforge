<script lang="ts">
	import { Card, Button, Badge, Alert, toaster } from "$ui";

	// There is no payment backend yet — this is the surface to wire one to.
	// Persist the chosen plan on the org (add a `plan` field) and drive upgrades
	// through your provider's checkout (e.g. Stripe) from choose().
	const currentPlan = "free";

	const plans = [
		{
			id: "free",
			name: "Free",
			price: "$0",
			period: "forever",
			features: ["1 organization", "Up to 3 members", "Community support"],
		},
		{
			id: "pro",
			name: "Pro",
			price: "$29",
			period: "/mo",
			featured: true,
			features: ["Unlimited organizations", "Up to 25 members", "Email support", "Advanced roles"],
		},
		{
			id: "business",
			name: "Business",
			price: "$99",
			period: "/mo",
			features: ["SSO / SAML", "Unlimited members", "Priority support", "Audit logs"],
		},
	];

	function choose(id: string) {
		if (id === currentPlan) return;
		toaster.warning("Billing isn't connected", "Wire up a payment provider (e.g. Stripe) to enable upgrades.");
	}
</script>

<h1 class="h1">Billing</h1>
<p class="forge-muted sub">You're on the <strong>{currentPlan}</strong> plan.</p>

<div class="plans">
	{#each plans as plan}
		<Card class={plan.featured ? "featured" : ""}>
			<div class="plan">
				<div class="ptop">
					<h3 class="pname">{plan.name}</h3>
					{#if plan.id === currentPlan}<Badge variant="secondary">Current</Badge>
					{:else if plan.featured}<Badge>Popular</Badge>{/if}
				</div>
				<div class="price"><span class="amount">{plan.price}</span><span class="period">{plan.period}</span></div>
				<ul class="features">
					{#each plan.features as f}<li>{f}</li>{/each}
				</ul>
				<Button
					class="full"
					variant={plan.id === currentPlan ? "outline" : plan.featured ? "default" : "outline"}
					disabled={plan.id === currentPlan}
					onclick={() => choose(plan.id)}
				>
					{plan.id === currentPlan ? "Current plan" : `Upgrade to ${plan.name}`}
				</Button>
			</div>
		</Card>
	{/each}
</div>

<Alert class="note">
	<strong>Developer note:</strong> billing is a UI stub. To make it real, add a
	<code>plan</code> field to the <code>orgs</code> collection and connect a payment
	provider's checkout in <code>choose()</code>.
</Alert>

<style>
	.h1 { font-size: 1.5rem; font-weight: 700; margin: 0; }
	.sub { margin: 0.25rem 0 1.5rem; }
	.plans { display: grid; grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); gap: 1rem; }
	:global(.card.featured) { border-color: hsl(var(--primary)); box-shadow: 0 0 0 1px hsl(var(--primary) / 0.3); }
	.plan { display: flex; flex-direction: column; gap: 1rem; height: 100%; }
	.ptop { display: flex; align-items: center; justify-content: space-between; }
	.pname { font-size: 1.1rem; font-weight: 600; }
	.price { display: flex; align-items: baseline; gap: 0.3rem; }
	.amount { font-size: 1.9rem; font-weight: 800; }
	.period { color: hsl(var(--muted-foreground)); font-size: 0.875rem; }
	.features { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 0.5rem; flex: 1; }
	.features li { font-size: 0.875rem; padding-left: 1.25rem; position: relative; }
	.features li::before { content: "✓"; position: absolute; left: 0; color: hsl(var(--primary)); font-weight: 700; }
	:global(.note) { margin-top: 1.5rem; }
	code { background: hsl(var(--muted)); padding: 1px 5px; border-radius: 4px; font-size: 0.85em; }
</style>
