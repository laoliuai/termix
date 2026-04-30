const ownerEmail = "liujia.gl@gmail.com";

export function SiteFooter() {
  return (
    <footer class="site-footer">
      <a class="site-footer-link" href={`mailto:${ownerEmail}`}>
        <img
          class="site-footer-avatar"
          src="/icons/owner-avatar-96.png"
          srcSet="/icons/owner-avatar-96.png 1x, /icons/owner-avatar-192.png 2x"
          width="32"
          height="32"
          alt=""
          aria-hidden="true"
        />
        <span>{ownerEmail}</span>
      </a>
    </footer>
  );
}
