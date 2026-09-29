# Running Go Canvas on Replit

This project uses its existing Go backend and React/Vite frontend. It requires Go 1.24+ and Node.js 22.12+. No database or API keys are needed.

- Install the frontend's locked dependencies with `npm --prefix frontend ci`.
- Run the **Start application** workflow (`npm run dev`) to open the frontend in Replit preview on port 5000. Its `/api` requests are proxied to the Go backend listening only on `127.0.0.1:8080`. The first run compiles the backend into `.cache/`.
- For local development outside Replit, `npm run dev` also serves at `http://127.0.0.1:5000`.
- Run `npm test` for Go and frontend checks. `npm run build` builds production assets; `npm start` runs the built service locally on port 8080.