#!/usr/bin/env fish
#
# Download the external datasets and verify them against the recorded checksums.
#
#   scripts/fetch_data.fish
#
# The checksums in data/external/SHA256SUMS pin the exact files the enrichment was built
# and hand-checked against. If an upstream file changes, this fails rather than silently
# enriching from different data -- the same reason external_source stores the hash.

set -l dir data/external
mkdir -p $dir

set -l names   ifct2017_index.csv                indian_food_dataset.csv
set -l urls \
    "https://huggingface.co/datasets/NUTRIC/IFCT-2017-Data/resolve/main/compositions-master/index.csv" \
    "https://huggingface.co/datasets/Anupam007/indian-recipe-dataset/resolve/main/Cleaned_Indian_Food_Dataset.csv"

for i in (seq (count $names))
    set -l name $names[$i]
    if test -f $dir/$name
        echo "have $name"
        continue
    end
    echo "fetching $name"
    curl -sSL -o $dir/$name $urls[$i]
    or begin
        echo "could not fetch $name" >&2
        exit 1
    end
end

if test -f $dir/SHA256SUMS
    pushd $dir >/dev/null
    sha256sum -c SHA256SUMS
    set -l ok $status
    popd >/dev/null
    if test $ok -ne 0
        echo "" >&2
        echo "checksum mismatch: an upstream dataset changed." >&2
        echo "re-run the enrichment sample check before updating SHA256SUMS." >&2
        exit 1
    end
else
    echo "no SHA256SUMS yet; recording current files"
    pushd $dir >/dev/null
    sha256sum *.csv >SHA256SUMS
    popd >/dev/null
end

echo "datasets ready in $dir"
