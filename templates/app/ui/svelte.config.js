import adapter from "@sveltejs/adapter-static";
import { vitePreprocess } from "@sveltejs/vite-plugin-svelte";

/** @type {import('@sveltejs/kit').Config} */
export default {
	preprocess: vitePreprocess(),
	kit: {
		// Build a fully static SPA that the Go binary embeds and serves.
		adapter: adapter({ fallback: "index.html", pages: "build", assets: "build" }),
		alias: { $ui: "src/lib/components/ui" },
	},
};
