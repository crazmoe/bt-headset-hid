# Package repository

`.github/workflows/repo.yml` builds an apt and a dnf repository from the deb and rpm packages of
all releases and publishes it on GitHub Pages at <https://crazmoe.github.io/bt-headset-hid/>. It
runs after each release and by hand (Actions, "Package repository", "Run workflow").

The packages built by `release.yml` add the repository themselves (`/etc/apt/sources.list.d/bt-headset-hid.sources` or
`/etc/yum.repos.d/bt-headset-hid.repo`, key in `/usr/share/keyrings/bt-headset-hid.asc`), so
whoever installs a package gets later versions with the system updates.

## Replacing the key

The repository is signed with the key in the secret `REPO_SIGNING_KEY` (Settings, Secrets and
variables, Actions); its public part is `packaging/repo-key.asc`. To replace it, create a new key
without a passphrase:

```
export GNUPGHOME=$(mktemp -d)
gpg --batch --passphrase '' --quick-gen-key "bt-headset-hid package repository" rsa4096 sign never
gpg --armor --export-secret-keys > repo-signing-key.asc
gpg --armor --export > repo-key.asc
```

Put `repo-signing-key.asc` into the secret, commit `repo-key.asc` as `packaging/repo-key.asc`,
then delete both files and the `GNUPGHOME` folder and run the workflow by hand. Installed
packages keep the old key until they are updated, so they cannot update across the change:
their users add the repository by hand once, as below.

## Adding it by hand

For packages of 0.4.0 and earlier, or after the key was replaced:

```
# Debian, Ubuntu
sudo curl -fsSLo /usr/share/keyrings/bt-headset-hid.asc https://crazmoe.github.io/bt-headset-hid/key.asc
printf 'Types: deb\nURIs: https://crazmoe.github.io/bt-headset-hid/apt/\nSuites: ./\nSigned-By: /usr/share/keyrings/bt-headset-hid.asc\n' |
  sudo tee /etc/apt/sources.list.d/bt-headset-hid.sources
sudo apt update && sudo apt install bt-headset-hid

# Fedora
sudo curl -fsSLo /etc/yum.repos.d/bt-headset-hid.repo https://crazmoe.github.io/bt-headset-hid/rpm/bt-headset-hid.repo
sudo dnf install bt-headset-hid
```

The repository metadata is signed (apt: `InRelease`, dnf: `repomd.xml.asc`); the rpm packages
themselves are not, so the `.repo` file sets `gpgcheck=0` and `repo_gpgcheck=1`.
