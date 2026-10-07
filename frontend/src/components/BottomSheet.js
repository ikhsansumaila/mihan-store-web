import React, { useEffect, useRef } from 'react';

// Bottom sheet yang bisa dipakai ulang: naik dari bawah (HP), dialog terpusat maks. 480px (md ke atas).
// Aksesibilitas: role="dialog" + aria-modal + aria-labelledby; tutup dengan Escape, ketuk latar, atau
// tombol di dalam (onClose); fokus pindah ke sheet saat dibuka, Tab berputar di dalam sheet, dan fokus
// kembali ke pemicu saat ditutup; scroll body dikunci selama terbuka.
// Animasi naik singkat dimatikan bila pengguna memilih prefers-reduced-motion.

// Keyframes disisipkan dari komponen (index.css sengaja hanya berisi gaya global HP).
const SHEET_CSS = `
@keyframes mihan-sheet-up { from { transform: translateY(24px); opacity: 0; } to { transform: none; opacity: 1; } }
@keyframes mihan-sheet-fade { from { opacity: 0; } to { opacity: 1; } }
.mihan-sheet-panel { animation: mihan-sheet-up 180ms ease-out; }
.mihan-sheet-backdrop { animation: mihan-sheet-fade 180ms ease-out; }
@media (prefers-reduced-motion: reduce) {
  .mihan-sheet-panel, .mihan-sheet-backdrop { animation: none; }
}
`;

const FOCUSABLE = 'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

const BottomSheet = ({ open, onClose, labelledBy, children, testId = 'bottom-sheet' }) => {
  const panelRef = useRef(null);
  const onCloseRef = useRef(onClose);
  onCloseRef.current = onClose;

  useEffect(() => {
    if (!open) return undefined;
    const returnTo = document.activeElement;
    const prevOverflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    if (panelRef.current) panelRef.current.focus();

    const onKey = (e) => {
      if (e.key === 'Escape') {
        e.stopPropagation();
        onCloseRef.current?.();
        return;
      }
      if (e.key !== 'Tab' || !panelRef.current) return;
      const items = [...panelRef.current.querySelectorAll(FOCUSABLE)];
      if (items.length === 0) {
        e.preventDefault();
        return;
      }
      const first = items[0];
      const last = items[items.length - 1];
      if (e.shiftKey && (document.activeElement === first || document.activeElement === panelRef.current)) {
        e.preventDefault();
        last.focus();
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault();
        first.focus();
      }
    };
    document.addEventListener('keydown', onKey, true);
    return () => {
      document.removeEventListener('keydown', onKey, true);
      document.body.style.overflow = prevOverflow;
      if (returnTo && typeof returnTo.focus === 'function' && document.contains(returnTo)) returnTo.focus();
    };
  }, [open]);

  if (!open) return null;
  return (
    <div className="fixed inset-0 z-50 flex items-end justify-center md:items-center md:p-4" data-testid={testId}>
      <style>{SHEET_CSS}</style>
      <div className="mihan-sheet-backdrop absolute inset-0 bg-black/50" aria-hidden="true" data-testid={`${testId}-backdrop`} onClick={() => onCloseRef.current?.()} />
      <div
        ref={panelRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={labelledBy}
        tabIndex={-1}
        className="mihan-sheet-panel relative w-full max-h-[85vh] overflow-y-auto rounded-t-2xl bg-white px-5 pt-4 shadow-xl focus:outline-none md:max-w-[480px] md:rounded-2xl"
        style={{ paddingBottom: 'calc(1.25rem + env(safe-area-inset-bottom, 0px))' }}
      >
        {/* Pegangan visual sheet (HP saja). */}
        <div aria-hidden="true" className="mx-auto mb-3 h-1.5 w-10 rounded-full bg-gray-300 md:hidden" />
        {children}
      </div>
    </div>
  );
};

export default BottomSheet;
