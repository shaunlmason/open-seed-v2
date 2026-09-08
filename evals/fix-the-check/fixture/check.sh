#!/bin/sh
# The acceptance check: greet.sh must print exactly the greeting.
dir=$(dirname "$0")
out=$(sh "$dir/greet.sh")
if [ "$out" != "hello, world" ]; then
  printf 'expected "hello, world", got "%s"\n' "$out" >&2
  exit 1
fi
printf 'ok\n'
