#!/bin/sh
set -eu

VERSION=${VERSION:?Set VERSION to a stable tag such as v0.3.0}
CHECKSUMS=${CHECKSUMS:-dist/checksums.txt}
OUTPUT=${OUTPUT:-dist/xswap.rb}
REPOSITORY=${REPOSITORY:-BryanPinheiro77/xswap}

printf '%s\n' "$VERSION" | grep -Eq '^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'
printf '%s\n' "$REPOSITORY" | grep -Eq '^[A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9_.-]*[A-Za-z0-9_][A-Za-z0-9_.-]*$'
test -f "$CHECKSUMS"

checksum() {
  name=$1
  value=$(awk -v expected="./$name" '$2 == expected { print $1 }' "$CHECKSUMS")
  test "${#value}" -eq 64
  printf '%s' "$value"
}

darwin_arm64=$(checksum "xswap_${VERSION}_darwin_arm64.tar.gz")
darwin_amd64=$(checksum "xswap_${VERSION}_darwin_amd64.tar.gz")
linux_arm64=$(checksum "xswap_${VERSION}_linux_arm64.tar.gz")
linux_amd64=$(checksum "xswap_${VERSION}_linux_amd64.tar.gz")
formula_version=${VERSION#v}
mkdir -p "$(dirname "$OUTPUT")"

cat > "$OUTPUT" <<EOF
class Xswap < Formula
  desc "Account switcher and quota monitor for Codex CLI"
  homepage "https://github.com/${REPOSITORY}"
  version "${formula_version}"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/${REPOSITORY}/releases/download/${VERSION}/xswap_${VERSION}_darwin_arm64.tar.gz"
      sha256 "${darwin_arm64}"
    else
      url "https://github.com/${REPOSITORY}/releases/download/${VERSION}/xswap_${VERSION}_darwin_amd64.tar.gz"
      sha256 "${darwin_amd64}"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/${REPOSITORY}/releases/download/${VERSION}/xswap_${VERSION}_linux_arm64.tar.gz"
      sha256 "${linux_arm64}"
    else
      url "https://github.com/${REPOSITORY}/releases/download/${VERSION}/xswap_${VERSION}_linux_amd64.tar.gz"
      sha256 "${linux_amd64}"
    end
  end

  def install
    libexec.install "xswap" => "xswap-bin"
    (bin/"xswap").write <<~SH
      #!/bin/sh
      case "\$(basename "\$0")" in
        codex) set -- __codex "\$@" ;;
      esac
      export XSWAP_PACKAGE_MANAGER=homebrew
      export XSWAP_EXECUTABLE="#{HOMEBREW_PREFIX}/opt/xswap/bin/xswap"
      exec "#{libexec}/xswap-bin" "\$@"
    SH
    (bin/"xswap").chmod 0755
    bin.install_symlink "xswap" => "codex-swap"
  end

  def caveats
    <<~EOS
      The official Codex CLI must be installed before configuring XSwap.
      Run \`xswap install\` once to connect XSwap to the official Codex CLI.
      XSwap installed by Homebrew is updated with \`brew upgrade xswap\`.
      Before removing the formula, run \`xswap uninstall\` to restore Codex.
    EOS
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/xswap version")
    assert_match "Managed by: Homebrew", shell_output("#{bin}/xswap version")
  end
end
EOF
