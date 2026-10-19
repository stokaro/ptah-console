// Lighthouse CI, modeled on denisvmedia/inventario. LHCI serves dist/ itself
// with a single-page fallback, so no backend is needed. Pages that need the
// API join the URL list as their steps land, served by a fixture backend.
module.exports = {
  ci: {
    collect: {
      staticDistDir: "./dist",
      isSinglePageApplication: true,
      url: ["http://localhost/"],
      // The median of three runs, so one noisy shared runner does not fail it.
      numberOfRuns: 3,
      settings: { preset: "desktop" },
    },
    assert: {
      assertions: {
        "categories:performance": ["error", { minScore: 0.9 }],
        "categories:accessibility": ["error", { minScore: 0.95 }],
        "categories:best-practices": ["error", { minScore: 0.9 }],
        "categories:seo": "off",
        "categories:pwa": "off",
      },
    },
    upload: { target: "temporary-public-storage" },
  },
}
