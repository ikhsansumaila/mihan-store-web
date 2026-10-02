import React from 'react';
import { Link } from 'react-router-dom';

const UPDATED = '2 Oktober 2026';
const CONTACT = 'halo@mihan.web.id';

const Page = ({ title, children }) => (
  <div className="max-w-3xl mx-auto px-4 py-10">
    <article className="bg-white rounded-2xl shadow p-6 sm:p-10 text-gray-700 leading-relaxed">
      <h1 className="text-3xl font-bold text-gray-900 mb-2">{title}</h1>
      <p className="text-sm text-gray-500 mb-8">Terakhir diperbarui: {UPDATED}</p>
      <div className="space-y-6">{children}</div>
    </article>
  </div>
);

const H = ({ children }) => <h2 className="text-xl font-semibold text-gray-900 mt-2 mb-2">{children}</h2>;
const UL = ({ children }) => <ul className="list-disc pl-6 space-y-1">{children}</ul>;
const Mail = () => (
  <a href={`mailto:${CONTACT}`} className="text-purple-700 underline">
    {CONTACT}
  </a>
);

export const PrivacyPolicy = () => (
  <Page title="Kebijakan Privasi">
    <p>
      Kebijakan ini menjelaskan bagaimana Mihan Store (<span className="whitespace-nowrap">store.mihan.web.id</span>)
      mengumpulkan, memakai, dan melindungi data Anda saat memakai situs dan akun toko kami. Mihan Store adalah toko
      daring kecil untuk produk makanan dan perlengkapan seperti kerupuk, tepung, saos, sambal, sendok plastik, dan box
      hampers.
    </p>

    <section>
      <H>1. Data yang kami kumpulkan</H>
      <UL>
        <li>
          <strong>Data akun:</strong> email, nama, username, dan nomor telepon/WhatsApp (opsional) yang Anda isi saat
          mendaftar.
        </li>
        <li>
          <strong>Data dari Google (bila Anda memilih “Masuk dengan Google”):</strong> alamat email beserta status
          verifikasinya, nama, foto profil dasar, dan ID akun Google. Kami tidak menerima password Google Anda dan tidak
          meminta akses ke Gmail, Drive, kontak, atau data Google lainnya.
        </li>
        <li>
          <strong>Password:</strong> bila Anda mendaftar dengan password, kami hanya menyimpan hasil hash satu arah
          (argon2id), bukan password aslinya.
        </li>
        <li>
          <strong>Data pesanan dan alamat pengiriman:</strong> saat Anda membuat pesanan, kami menyimpan nama penerima,
          nomor telepon penerima, alamat, kota, kode pos (opsional), dan catatan pesanan (opsional), beserta isi pesanan
          (produk, jumlah, harga saat dipesan), diskon, ongkir, total, status pesanan, dan waktu perubahannya. Isi
          keranjang belanja juga disimpan di server kami selama Anda login.
        </li>
        <li>
          <strong>Catatan aktivitas keamanan:</strong> waktu login/logout, percobaan login yang gagal, alamat IP, dan
          informasi perangkat/peramban (user agent). Catatan ini disimpan paling lama sekitar 6 bulan.
        </li>
      </UL>
    </section>

    <section>
      <H>2. Untuk apa data dipakai</H>
      <UL>
        <li>Membuat dan mengelola akun Anda serta membuat Anda tetap masuk (sesi login).</li>
        <li>Menjaga keamanan: mencegah penyalahgunaan, menebak password, dan akun palsu.</li>
        <li>Menjalankan layanan toko, misalnya menampilkan produk dan menghubungi Anda terkait pesanan.</li>
        <li>
          Memproses pesanan: menghitung total, mengonfirmasi pembayaran transfer, mengirim barang ke alamat yang Anda isi,
          dan menghubungi Anda (misalnya lewat WhatsApp) tentang pesanan tersebut.
        </li>
      </UL>
      <p>
        <strong>Siapa yang bisa melihat data pesanan:</strong> Anda sendiri (lewat menu “Pesanan Saya”) dan pemilik/admin
        Mihan Store yang mengelola pesanan. Data pesanan tidak ditampilkan kepada pengguna lain.
      </p>
    </section>

    <section>
      <H>3. Berbagi data</H>
      <p>
        Kami <strong>tidak menjual</strong> data pribadi Anda kepada pihak ketiga. Data hanya diproses oleh penyedia
        layanan yang membantu situs ini berjalan:
      </p>
      <UL>
        <li>
          <strong>Google Sign-In</strong> — untuk login dengan akun Google (berlaku juga kebijakan privasi Google).
        </li>
        <li>
          <strong>Cloudflare</strong> — jaringan, perlindungan keamanan situs, dan kontrol akses halaman admin.
        </li>
        <li>
          <strong>Cloudflare Turnstile</strong> — verifikasi bahwa Anda bukan robot pada formulir login dan daftar.
        </li>
        <li>
          <strong>Discord</strong> — notifikasi internal ke pengelola toko saat ada pesanan baru, dibayar, atau
          dibatalkan. Notifikasi ini <strong>hanya</strong> memuat nomor pesanan, nama pemesan, jumlah item, total, dan
          status; <strong>tidak</strong> memuat alamat, nomor telepon, email, atau catatan pesanan.
        </li>
        <li>
          <strong>WhatsApp</strong> — bila Anda menekan tombol “Konfirmasi via WhatsApp”, aplikasi WhatsApp dibuka dengan
          ringkasan pesanan yang Anda kirim sendiri ke toko (berlaku juga kebijakan privasi WhatsApp). Admin juga dapat
          mengirim ringkasan pesanan ke nomor telepon penerima lewat WhatsApp.
        </li>
      </UL>
      <p>Kami juga dapat mengungkapkan data bila diwajibkan oleh hukum yang berlaku.</p>
    </section>

    <section>
      <H>4. Penyimpanan dan keamanan</H>
      <p>
        Data disimpan di server yang kami kelola. Kami memakai koneksi terenkripsi (HTTPS), hash password, token sesi
        yang hanya disimpan dalam bentuk hash, pembatasan percobaan login, dan pembatasan akses admin. Tidak ada sistem
        yang sepenuhnya bebas risiko, tetapi kami berupaya melindungi data Anda secara wajar.
      </p>
      <p>
        <strong>Masa penyimpanan data pesanan:</strong> data pesanan (termasuk data penerima dan alamat pengiriman)
        disimpan untuk keperluan pencatatan dan pembukuan toko, termasuk setelah pesanan selesai atau dibatalkan. Isi
        keranjang dihapus setelah pesanan dibuat atau saat Anda menghapusnya. Anda dapat meminta penghapusan data sesuai
        bagian “Hak Anda”, kecuali data yang perlu kami simpan untuk pencatatan transaksi atau menurut hukum.
      </p>
    </section>

    <section>
      <H>5. Hak Anda</H>
      <UL>
        <li>Meminta salinan data akun Anda.</li>
        <li>Meminta perbaikan data yang tidak tepat.</li>
        <li>Meminta penghapusan akun dan data terkait (kecuali yang wajib kami simpan menurut hukum).</li>
        <li>Mencabut akses Mihan Store dari akun Google Anda melalui pengaturan keamanan akun Google.</li>
      </UL>
      <p>
        Kirim permintaan ke <Mail />. Kami akan menanggapi dalam waktu yang wajar.
      </p>
    </section>

    <section>
      <H>6. Cookie dan penyimpanan lokal</H>
      <p>
        Situs memakai penyimpanan lokal peramban untuk menyimpan sesi login Anda (keranjang belanja disimpan di server,
        bukan di peramban). Cloudflare dapat memasang cookie yang
        diperlukan untuk keamanan. Kami tidak memakai cookie iklan.
      </p>
    </section>

    <section>
      <H>7. Perubahan kebijakan</H>
      <p>
        Kebijakan ini dapat diperbarui sewaktu-waktu. Tanggal “Terakhir diperbarui” di atas menunjukkan versi terbaru.
      </p>
    </section>

    <section>
      <H>8. Kontak</H>
      <p>
        Pertanyaan tentang privasi dapat dikirim ke <Mail />. Lihat juga{' '}
        <Link to="/syarat" className="text-purple-700 underline">
          Syarat &amp; Ketentuan
        </Link>
        .
      </p>
    </section>
  </Page>
);

export const TermsOfService = () => (
  <Page title="Syarat & Ketentuan">
    <p>
      Dengan memakai situs Mihan Store (<span className="whitespace-nowrap">store.mihan.web.id</span>) Anda menyetujui
      syarat berikut. Bila tidak setuju, mohon tidak memakai layanan ini.
    </p>

    <section>
      <H>1. Akun</H>
      <UL>
        <li>Anda dapat mendaftar dengan email/username dan password, atau masuk dengan akun Google.</li>
        <li>Data yang Anda berikan harus benar. Satu email hanya untuk satu akun.</li>
        <li>
          Anda bertanggung jawab menjaga kerahasiaan password dan perangkat Anda. Beri tahu kami segera bila ada
          penggunaan akun yang mencurigakan.
        </li>
        <li>
          Bila Anda masuk dengan Google memakai email yang sudah terdaftar, akun tersebut akan disambungkan ke Google dan
          selanjutnya login dilakukan dengan Google.
        </li>
      </UL>
    </section>

    <section>
      <H>2. Produk dan harga</H>
      <UL>
        <li>Informasi produk, harga, dan ketersediaan dapat berubah sewaktu-waktu tanpa pemberitahuan.</li>
        <li>Foto dan deskripsi produk adalah gambaran umum; tampilan sebenarnya bisa sedikit berbeda.</li>
        <li>Harga ditampilkan dalam Rupiah (Rp).</li>
      </UL>
    </section>

    <section>
      <H>3. Pemesanan dan pembayaran</H>
      <UL>
        <li>Pemesanan dilakukan setelah login: tambahkan produk ke keranjang, isi data penerima, lalu buat pesanan.</li>
        <li>
          Pembayaran dengan transfer bank manual ke rekening yang tertera di halaman pesanan. Ongkir dan diskon (bila ada)
          ditetapkan admin; total akhir terlihat di halaman pesanan.
        </li>
        <li>
          Pesanan yang belum dibayar dapat Anda batalkan sendiri. Status pesanan diperbarui admin setelah pembayaran
          dikonfirmasi; pengiriman dan invoice disepakati dengan Mihan Store (misalnya lewat WhatsApp).
        </li>
      </UL>
    </section>

    <section>
      <H>4. Larangan</H>
      <UL>
        <li>Mencoba membobol, mengganggu, atau membebani situs secara berlebihan.</li>
        <li>Membuat akun palsu, meniru orang lain, atau memakai akun orang lain tanpa izin.</li>
        <li>Memakai situs untuk tindakan yang melanggar hukum.</li>
      </UL>
      <p>Kami dapat menangguhkan atau menutup akun yang melanggar ketentuan ini.</p>
    </section>

    <section>
      <H>5. Batasan tanggung jawab</H>
      <p>
        Situs disediakan “sebagaimana adanya”. Kami berupaya menjaga situs tetap tersedia dan akurat, tetapi tidak
        menjamin situs selalu bebas gangguan atau kesalahan.
      </p>
    </section>

    <section>
      <H>6. Privasi</H>
      <p>
        Pemakaian data pribadi diatur dalam{' '}
        <Link to="/privasi" className="text-purple-700 underline">
          Kebijakan Privasi
        </Link>
        .
      </p>
    </section>

    <section>
      <H>7. Perubahan dan hukum yang berlaku</H>
      <p>
        Syarat ini dapat diperbarui sewaktu-waktu; versi terbaru selalu tersedia di halaman ini. Syarat ini tunduk pada
        hukum Republik Indonesia.
      </p>
    </section>

    <section>
      <H>8. Kontak</H>
      <p>
        Pertanyaan dapat dikirim ke <Mail />.
      </p>
    </section>
  </Page>
);
