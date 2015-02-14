#!/usr/bin/env python
# Copyright 2015 Marc-Antoine Ruel. All rights reserved.
# Use of this source code is governed under the Apache License, Version 2.0
# that can be found in the LICENSE file.

# Copyright 2013 The Swarming Authors. All rights reserved.
# Use of this source code is governed by the Apache v2.0 license that can be
# found in the LICENSE file.
# Source:
# https://code.google.com/p/swarming/source/browse/appengine/components/tools/calculate_version.py

"""Uploads."""

import getpass
import logging
import optparse
import os
import subprocess
import sys

SEE_ALL_DIR = os.path.join(
    os.path.dirname(os.path.abspath(__file__)), 'seeall')


# Update as needed.
GO_APPENGINE = os.path.expanduser('~/src/go_appengine')


def git(cmd, cwd):
  return subprocess.check_output(['git'] + cmd, cwd=cwd)


def get_pseudo_revision(root, remote):
  mergebase = git(['merge-base', 'HEAD', remote], cwd=root).rstrip()
  pseudo_rev = len(git(['log', mergebase, '--format="%h"'], root).splitlines())
  return pseudo_rev, mergebase


def is_pristine(root, mergebase):
  """Returns True if the tree is pristine relating to mergebase."""
  head = git(['rev-parse', 'HEAD'], cwd=root).rstrip()
  if head != mergebase:
    return False

  # Look for local uncommitted diff.
  return not (
      git(['diff', '--ignore-submodules=none', mergebase], cwd=root) or
      git(['diff', '--ignore-submodules', '--cached', mergebase], cwd=root))


def calculate_version(root, tag):
  pseudo_revision, mergebase = get_pseudo_revision(root, 'origin/master')
  pristine = is_pristine(root, mergebase)
  version = '%s-%s' % (pseudo_revision, mergebase[:7])
  if not pristine:
    version += '-tainted-%s' % getpass.getuser()
  if tag:
    version += '-' + tag
  return version


def checkout_root(cwd):
  """Returns the root of the checkout."""
  return git(['rev-parse', '--show-toplevel'], cwd).rstrip()


def main():
  parser = optparse.OptionParser(description=sys.modules[__name__].__doc__)
  parser.add_option('-v', '--verbose', action='store_true')
  parser.add_option(
      '-t', '--tag', help='Tag to attach to a tainted version')
  options, args = parser.parse_args()
  logging.basicConfig(level=logging.DEBUG if options.verbose else logging.ERROR)

  if args:
    parser.error('Unknown arguments, %s' % args)

  root = checkout_root(os.getcwd())
  logging.info('Checkout root is %s', root)
  version = calculate_version(root, options.tag)
  print version

  # '--oauth2',
  cmd = [
    os.path.join(GO_APPENGINE, 'appcfg.py'), 'update', SEE_ALL_DIR,
    '-V', version,
  ]
  ret = subprocess.call(cmd)
  if ret:
    return ret
  cmd = [
    os.path.join(GO_APPENGINE, 'appcfg.py'), 'set_default_version', SEE_ALL_DIR,
    '-V', version,
  ]
  return subprocess.call(cmd)


if __name__ == '__main__':
  sys.exit(main())
