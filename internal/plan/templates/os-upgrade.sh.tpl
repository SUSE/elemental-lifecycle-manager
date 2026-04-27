set -e

HOST="${HOST:-/host}"
DEPLOYMENT="${DEPLOYMENT:-$HOST/etc/elemental/deployment.yaml}"
OS_IMAGE_REPO="{{ .OSImageRepo }}"
OS_VERSION="{{ .OSImageVersion }}"
INCOMING="$OS_IMAGE_REPO:$OS_VERSION"
CURRENT=$(grep -F "uri: oci://$OS_IMAGE_REPO" "$DEPLOYMENT" 2>/dev/null || true)

# On fresh systems, we have a sourceOS specified with raw (e.g. raw://../squashfs.img) data
# instead of from an OCI image, so for instances that CURRENT is empty we
# assume that this is a fresh system and proceed with the upgrade.
if [ -n "$CURRENT" ]; then
	# Extract the prefix (e.g. "uri: oci://") before the OS_IMAGE_REPO,
	# so that it can be stripped in the next step.
    prefix=${CURRENT%%"$OS_IMAGE_REPO"*}
	CURRENT=${CURRENT#"$prefix"}
    if [ "$CURRENT" = "$INCOMING" ]; then
        echo "Active OS image is already at correct version $OS_VERSION. Upgrade has been performed."
        exit 0
    fi
fi

{{ if .FetchRemote }}USE_LOCAL_IMAGES=false {{ end }}upgrader "$INCOMING" && chroot /host reboot
