// IntersectionObserver tiruan untuk jsdom (yang tidak memilikinya). Tes memanggil intersect() untuk
// mensimulasikan sentinel daftar gulir-tanpa-batas masuk ke layar.
const observers = new Set();

class MockIntersectionObserver {
  constructor(callback, options) {
    this.callback = callback;
    this.options = options;
    this.targets = new Set();
    observers.add(this);
  }

  observe(el) {
    this.targets.add(el);
  }

  unobserve(el) {
    this.targets.delete(el);
  }

  disconnect() {
    this.targets.clear();
    observers.delete(this);
  }
}

export const installIntersectionObserver = () => {
  observers.clear();
  window.IntersectionObserver = MockIntersectionObserver;
};

export const uninstallIntersectionObserver = () => {
  observers.clear();
  delete window.IntersectionObserver;
};

export const activeObservers = () => [...observers];

// Semua target yang diamati dianggap terlihat (isIntersecting = visible).
export const intersect = (visible = true) => {
  [...observers].forEach((o) => {
    const entries = [...o.targets].map((target) => ({ target, isIntersecting: visible, intersectionRatio: visible ? 1 : 0 }));
    if (entries.length) o.callback(entries, o);
  });
};
