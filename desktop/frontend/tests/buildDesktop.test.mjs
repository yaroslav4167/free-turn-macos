import {test} from 'node:test'
import assert from 'node:assert/strict'
import {mkdtempSync, mkdirSync, readFileSync, writeFileSync, cpSync, rmSync, existsSync} from 'node:fs'
import {tmpdir} from 'node:os'
import {join} from 'node:path'
import {fileURLToPath} from 'node:url'
import {spawnSync} from 'node:child_process'
import {gunzipSync} from 'node:zlib'
import {createHash} from 'node:crypto'

const root = fileURLToPath(new URL('../../../', import.meta.url))

test('workflows use Node 24-compatible actions without changing the frontend Node version', () => {
  for (const name of ['build.yml', 'release.yml', 'ci.yml']) {
    const workflow = readFileSync(join(root, '.github/workflows', name), 'utf8')
    assert.match(workflow, /actions\/setup-node@v7\.1\.0/)
    assert.match(workflow, /node-version: 22/)
    assert.doesNotMatch(workflow, /actions\/(?:setup-node|upload-artifact)@v4\b/)
  }
  assert.match(readFileSync(join(root, '.github/workflows/build.yml'), 'utf8'), /actions\/upload-artifact@v7\.0\.2/)
  assert.match(readFileSync(join(root, '.github/workflows/release.yml'), 'utf8'), /softprops\/action-gh-release@v3\.0\.3/)
})

test('GitHub workflows inject their own repository into update builds', () => {
  for (const name of ['build.yml', 'release.yml']) {
    const workflow = readFileSync(join(root, '.github/workflows', name), 'utf8')
    assert.match(workflow, /UPDATE_REPOSITORY: \$\{\{ github\.repository \}\}/)
  }
  const script = readFileSync(join(root, 'packaging/macos/build-desktop.sh'), 'utf8')
  assert.match(script, /-X main\.updateRepository=\$repository/)
  assert.match(script, /-X main\.desktopVersion=\$version/)
})

test('desktop build script packages both binaries, documentation and checksums', {skip: process.platform !== 'linux'}, (t) => {
  const temporary = mkdtempSync(join(tmpdir(), 'freeturn-desktop-build-'))
  t.after(() => rmSync(temporary, {recursive: true, force: true}))
  const repo = join(temporary, 'Repository with spaces')
  const tools = join(temporary, 'tools')
  mkdirSync(tools)
  for (const path of ['packaging/linux', 'desktop/assets', 'docs/screenshots', 'desktop/build/bin']) {
    mkdirSync(join(repo, path), {recursive: true})
  }
  cpSync(join(root, 'packaging/linux/build-desktop.sh'), join(repo, 'packaging/linux/build-desktop.sh'))
  for (const path of ['README.md', 'LICENSE', 'desktop/assets/icon.svg', 'packaging/linux/install-desktop.sh', 'packaging/linux/uninstall-desktop.sh', 'packaging/linux/freeturn.desktop', 'docs/screenshots/test.png', 'docs/localization.md']) {
    writeFileSync(join(repo, path), 'test fixture')
  }
  const stub = (name, content) => writeFileSync(join(tools, name), '#!/bin/sh\nset -eu\n' + content, {mode: 0o755})
  stub('go', `case "$*" in
  'env GOBIN') printf '%s' "$STUB_TOOLS" ;;
  'env GOARCH') echo amd64 ;;
  install*) printf '%s\\n' "$*" > "$STUB_REPO/install.log" ;;
  *) exit 1 ;;
esac\n`)
  stub('wails', `printf '%s\\n' "$*" > "$STUB_REPO/wails.log"
printf 'app' > build/bin/freeturn-desktop
if [ "\${SKIP_HELPER:-}" != 1 ]; then printf 'helper' > build/bin/freeturn-vpn-helper; fi\n`)
  stub('npm', `printf '%s\\n' "$*" > "$STUB_REPO/npm.log"\n`)
  stub('node', 'exit 0\n')
  stub('pkg-config', 'exit 0\n')
  const env = {...process.env, PATH: tools + ':' + process.env.PATH, STUB_TOOLS: tools, STUB_REPO: repo,
    UPDATE_REPOSITORY: 'example/custom-fork', VERSION: 'v2.3.4'}
  const script = join(repo, 'packaging/linux/build-desktop.sh')
  const run = spawnSync('sh', [script], {cwd: temporary, env, encoding: 'utf8'})
  assert.equal(run.status, 0, run.stderr)
  assert.match(readFileSync(join(repo, 'install.log'), 'utf8'), /wails@v2\.16\.0/)
  assert.match(readFileSync(join(repo, 'npm.log'), 'utf8'), /--prefix desktop\/frontend ci/)
  assert.match(readFileSync(join(repo, 'wails.log'), 'utf8'), /build -tags webkit2_41/)
  assert.match(readFileSync(join(repo, 'wails.log'), 'utf8'), /-X main.updateRepository=example\/custom-fork/)
  assert.match(readFileSync(join(repo, 'wails.log'), 'utf8'), /-X main.desktopVersion=v2\.3\.4/)
  const archive = join(repo, 'dist/FreeTurn-Linux-amd64.tar.gz')
  const tar = gunzipSync(readFileSync(archive))
  const entries = []
  for (let offset = 0; offset + 512 <= tar.length && tar[offset] !== 0;) {
    entries.push(tar.subarray(offset, offset + 100).toString().replace(/\0.*$/s, ''))
    const size = parseInt(tar.subarray(offset + 124, offset + 136).toString().replace(/\0.*$/s, '').trim(), 8) || 0
    offset += 512 + Math.ceil(size / 512) * 512
  }
  for (const entry of ['freeturn-desktop', 'freeturn-vpn-helper', 'install-desktop.sh', 'uninstall-desktop.sh', 'freeturn.desktop', 'desktop/assets/icon.svg', 'docs/screenshots/test.png', 'docs/localization.md']) {
    assert.ok(entries.includes('./' + entry), entry)
  }
  const digest = createHash('sha256').update(readFileSync(archive)).digest('hex')
  assert.equal(readFileSync(archive + '.sha256', 'utf8').trim(), `${digest}  FreeTurn-Linux-amd64.tar.gz`)
  rmSync(join(repo, 'desktop/build/bin/freeturn-vpn-helper'))
  const missing = spawnSync('sh', [script], {env: {...env, SKIP_HELPER: '1'}, encoding: 'utf8'})
  assert.notEqual(missing.status, 0)
  assert.match(missing.stderr, /did not produce freeturn-vpn-helper/)
  assert.ok(existsSync(archive))
})
