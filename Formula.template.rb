# Replace placeholders before publishing to <GH_USER>/homebrew-tap.
class <BIN> < Formula
  desc "Cautious all-in-one macOS utility"
  homepage "https://github.com/<GH_USER>/<BIN>"
  version "VERSION"
  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/<GH_USER>/<BIN>/releases/download/v#{version}/<BIN>_#{version}_Darwin_arm64.tar.gz"
      sha256 "SHA256_ARM64"
    else
      url "https://github.com/<GH_USER>/<BIN>/releases/download/v#{version}/<BIN>_#{version}_Darwin_x86_64.tar.gz"
      sha256 "SHA256_AMD64"
    end
  end
  license "GPL-3.0-or-later"
  def install
    bin.install "<BIN>"
  end
end
