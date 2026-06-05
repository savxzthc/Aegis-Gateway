import type { Config } from 'tailwindcss';

export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        base: 'var(--bg-base)',
        surface: 'var(--bg-surface)',
        elevated: 'var(--bg-elevated)',
        border: 'var(--border)',
        primary: 'var(--text-primary)',
        muted: 'var(--text-muted)',
        accent: 'var(--accent)',
        warn: 'var(--warn)',
        danger: 'var(--danger)',
        success: 'var(--success)',
      },
      fontFamily: {
        sans: ['JetBrains Mono', 'monospace'],
        mono: ['JetBrains Mono', 'monospace'],
      },
      borderRadius: {
        panel: '0px',
      },
    },
  },
  plugins: [],
} satisfies Config;
