import { sveltekit } from "@sveltejs/kit/vite";

export default {
	plugins: [sveltekit()],
	server: {
		// During `forge dev`, proxy API + admin + realtime to the Go server.
		proxy: {
			"/api": "http://localhost:8090",
			"/_": "http://localhost:8090",
		},
	},
};
