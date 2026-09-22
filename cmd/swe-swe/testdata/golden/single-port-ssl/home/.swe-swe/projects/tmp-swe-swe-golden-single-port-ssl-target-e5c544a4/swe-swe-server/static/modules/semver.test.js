/**
 * Unit tests for semver.js
 * Run with: node --test semver.test.js
 */

import { test } from 'node:test';
import assert from 'node:assert';
import { compareVersions, isNewer } from './semver.js';

test('compareVersions orders release cores numerically, not lexically', () => {
    assert.strictEqual(compareVersions('2.33.0', '2.34.0'), -1);
    assert.strictEqual(compareVersions('2.34.0', '2.33.0'), 1);
    assert.strictEqual(compareVersions('2.34.0', '2.34.0'), 0);
    // 9 < 10 numerically even though "10" sorts before "9" as text
    assert.strictEqual(compareVersions('2.9.0', '2.10.0'), -1);
    assert.strictEqual(compareVersions('2.34.2', '2.34.10'), -1);
});

test('compareVersions treats missing core segments as zero', () => {
    assert.strictEqual(compareVersions('2.34', '2.34.0'), 0);
    assert.strictEqual(compareVersions('2.34', '2.34.1'), -1);
    assert.strictEqual(compareVersions('3', '2.99.99'), 1);
});

test('compareVersions ranks a release above its own prereleases', () => {
    // the regression this module exists for: stripping "-rc1" made these equal,
    // so a box on 2.34.1-rc1 was never told 2.34.1 had shipped
    assert.strictEqual(compareVersions('2.34.1-rc1', '2.34.1'), -1);
    assert.strictEqual(compareVersions('2.34.1', '2.34.1-rc1'), 1);
});

test('compareVersions orders prerelease identifiers per semver 11.4', () => {
    assert.strictEqual(compareVersions('1.0.0-alpha', '1.0.0-beta'), -1);
    assert.strictEqual(compareVersions('1.0.0-alpha.1', '1.0.0-alpha.2'), -1);
    // numeric identifiers compare numerically, not as text
    assert.strictEqual(compareVersions('1.0.0-rc.9', '1.0.0-rc.10'), -1);
    // numeric identifiers rank below alphanumeric ones
    assert.strictEqual(compareVersions('1.0.0-1', '1.0.0-alpha'), -1);
    // a longer identifier list wins when the shared prefix is equal
    assert.strictEqual(compareVersions('1.0.0-alpha', '1.0.0-alpha.1'), -1);
    assert.strictEqual(compareVersions('1.0.0-alpha.1', '1.0.0-alpha.1'), 0);
});

test('compareVersions ignores build metadata and a leading v', () => {
    assert.strictEqual(compareVersions('2.34.0+build.1', '2.34.0+build.9'), 0);
    assert.strictEqual(compareVersions('v2.34.0', '2.34.0'), 0);
    assert.strictEqual(compareVersions('v2.33.0', '2.34.0'), -1);
});

test('isNewer only fires when the published version really is ahead', () => {
    assert.strictEqual(isNewer('2.33.0', '2.34.0'), true);
    assert.strictEqual(isNewer('2.34.0', '2.34.0'), false);
    assert.strictEqual(isNewer('2.34.0', '2.33.0'), false);
    // a prerelease box should be told about the stable release that supersedes it
    assert.strictEqual(isNewer('2.34.1-rc1', '2.34.1'), true);
    // but a published prerelease must not nag a box already on the release
    assert.strictEqual(isNewer('2.34.1', '2.34.1-rc1'), false);
});

test('isNewer stays quiet on unparseable input rather than badging wrongly', () => {
    assert.strictEqual(isNewer('dev', 'dev'), false);
    assert.strictEqual(isNewer('2.34.0', ''), false);
    assert.strictEqual(isNewer('2.34.0', 'not-a-version'), false);
});
