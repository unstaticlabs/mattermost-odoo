# Four frozen plugin dependency updates

Frozen on 12 November 2026 at 07:50 UTC, based on merged main
`1d0b4ed1b00b6c78a592a95ee873767435a01340`. Normal merge commits retain each
literal Renovate head. Later Renovate updates are outside this batch.

| PR | Dependency | Before → after | Frozen head |
|---|---|---|---|
| 7 | `@babel/preset-env` | 7.29.7 → 7.29.14 | `d03ee7cbaeeede7433f271afe322d27c52b20586` |
| 10 | `@babel/runtime` | 7.29.13 → 7.29.14 | `8eeb3ce22b61c32480ee5a4c39792cc0bd76a537` |
| 12 | `@emotion/babel-preset-css-prop` | 11.2.0 → 11.12.0 | `877114717bae58ae3ef4d08530d0ec79c90b772e` |
| 13 | `@emotion/react` | 11.9.0 → 11.14.0 | `9ce371da21aa42072504123fa35447ec5fca45ab` |

## Compatibility review

The existing Babel configuration uses classic JSX and puts the Emotion preset
after the React and TypeScript presets, as its [release-tag documentation](https://github.com/emotion-js/emotion/blob/d57cfcb6daf48fc5458f91b4db2e072fbc2863e4/packages/babel-preset-css-prop/README.md)
requires. It does not use the incompatible inline-elements transform. The
[preset changelog](https://github.com/emotion-js/emotion/blob/d57cfcb6daf48fc5458f91b4db2e072fbc2863e4/packages/babel-preset-css-prop/CHANGELOG.md)
records updated Babel/Emotion helpers and package exports.

Emotion React's [release-tag changelog](https://github.com/emotion-js/emotion/blob/3c19ce5997f73960679e546af47801205631dfde/packages/react/CHANGELOG.md)
introduces conditional exports, opt-in runtime labels and generated TypeScript
declarations across these versions. The plugin has no direct Emotion imports,
CSS props or runtime label dependency: its status panel uses native React inline
styles and existing CSS classes. Babel may inject Emotion's public JSX helper.
The [published peer requirement](https://github.com/emotion-js/emotion/blob/3c19ce5997f73960679e546af47801205631dfde/packages/react/package.json)
accepts the existing React 18.2.0. Webpack 5 remains the browser bundler and React
remains external. These observations support compatibility; combined compilation
and tests provide the execution evidence.

## Qualification scope

Conflicting declaration/lock hunks were resolved through the intended direct
versions and normal `npm install --package-lock-only --ignore-scripts --no-audit
--no-fund` under declared Node 24.13.1, without force or overrides. All four exact
heads are ancestors; direct declaration differences contain only those four
updates. The lock delta is limited to their entries, Emotion helper updates and
memoization deduplication. Product source, Babel/webpack configuration, Go files,
React and runtime versions are unchanged.

Local checks cover ancestry, JSON/declaration/lock scope and whitespace. Full
combined proof comes from the existing hosted Ubuntu workflow: native manifest
preparation, Go vet/race tests, locked npm install, lint/types/Jest, native bundle
build and verification of both Linux ELF architectures, web entry and size.
Individual green checks do not establish the combined branch's result; consult
its current PR check. No local heavy QA stack or service activation was started.

The preceding batch recorded 35 npm advisories. This batch makes no clean-audit
claim and adds no audit fix, force resolution or suppression. Fresh advisory
metadata, when available, is reported separately with the PR qualification.
