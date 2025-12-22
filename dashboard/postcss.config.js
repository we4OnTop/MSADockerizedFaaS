module.exports = {
  plugins: {
    tailwindcss: {},
    autoprefixer: {},
    "postcss-selector-replace": {
      before: /:root|:host/g,
      after: ".root"
    }
  }
};
