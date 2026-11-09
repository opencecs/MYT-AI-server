import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import wails from "@wailsio/runtime/plugins/vite";

export default defineConfig({
  resolve: {
    extensions: ['.ts', '.tsx', '.js', '.jsx'],
  },
  server: {
    // 必须用 IPv4 明确地址：wails 的 ExternalAssetHandler 用 tcp4 127.0.0.1 代理，
    // 若绑定 localhost(→IPv6 ::1) 会导致连接被拒 → HTTP 502 白屏
    host: '127.0.0.1',
  },
  plugins: [react(), tailwindcss(), wails("./bindings")],
  build: {
    target: 'esnext',
    rollupOptions: {
      output: {
        manualChunks: {
          'vendor-react': ['react', 'react-dom'],
          'vendor-wails': ['@wailsio/runtime'],
        }
      }
    }
  }
});
