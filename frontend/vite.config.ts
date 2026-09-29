import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
export default defineConfig({ plugins: [react()], server: { allowedHosts: true, proxy: { '/api': process.env.GOVIZ_API_TARGET || 'http://127.0.0.1:8080' } } });
