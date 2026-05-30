# Third-Party Notices

Aegis Gateway includes or depends on third-party open-source software.

## Backend Dependencies

- github.com/BurntSushi/toml - MIT
- github.com/go-chi/chi/v5 - MIT
- golang.org/x/crypto - BSD-3-Clause
- modernc.org/sqlite - BSD-3-Clause
- github.com/dustin/go-humanize - MIT
- github.com/google/uuid - BSD-3-Clause
- github.com/ncruces/go-strftime - MIT
- github.com/remyoudompheng/bigfft - BSD-3-Clause
- modernc.org/libc - BSD-3-Clause
- modernc.org/mathutil - BSD-3-Clause
- modernc.org/memory - BSD-3-Clause

## Frontend Dependencies

- React - MIT
- React DOM - MIT
- Axios - MIT
- Recharts - MIT
- Zustand - MIT
- React Hot Toast - MIT
- Lucide React - ISC
- Tailwind CSS - MIT
- Vite - MIT
- TypeScript - Apache-2.0
- @fontsource/dm-sans / DM Sans - SIL Open Font License 1.1
- @fontsource/jetbrains-mono / JetBrains Mono - SIL Open Font License 1.1

The full license texts for these dependencies are available in their respective upstream repositories and package distributions.

For fuller generated license reports before publishing binary releases, use tools such as:

```bash
cd frontend
npx license-checker --production --summary
npx license-checker --production --out ../THIRD_PARTY_LICENSES_FRONTEND.txt
```

```bash
go install github.com/google/go-licenses@latest
go-licenses report ./... > THIRD_PARTY_LICENSES_GO.txt
```
