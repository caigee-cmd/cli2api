import assert from 'node:assert/strict'
import { detectImportBundle, parseImportBundleText } from '../src/lib/accountImportDetect.ts'

function run(name: string, fn: () => void) {
  try {
    fn()
    console.log(`ok  ${name}`)
  } catch (error) {
    console.error(`FAIL ${name}`)
    throw error
  }
}

run('format workbuddy-accounts → workbuddy cn', () => {
  const result = detectImportBundle({
    format: 'workbuddy-accounts',
    accounts: [{ accessToken: 'a', domain: 'https://www.codebuddy.cn' }],
  })
  assert.equal(result.provider, 'workbuddy')
  assert.equal(result.format, 'workbuddy-accounts')
  assert.equal(result.region, 'cn')
  assert.equal(result.regionAmbiguous, false)
  assert.equal(result.batchImportSupported, true)
  assert.equal(result.confidence, 'high')
})

run('format workbuddy-oauth-v1 global domain', () => {
  const result = detectImportBundle({
    format: 'workbuddy-oauth-v1',
    accessToken: 'a',
    domain: 'https://www.workbuddy.ai',
  })
  assert.equal(result.provider, 'workbuddy')
  assert.equal(result.region, 'global')
  assert.equal(result.batchImportSupported, true)
})

run('mixed CN+Global → ambiguous', () => {
  const result = detectImportBundle({
    format: 'workbuddy-accounts',
    accounts: [
      { accessToken: 'a', domain: 'codebuddy.cn' },
      { accessToken: 'b', domain: 'workbuddy.ai' },
    ],
  })
  assert.equal(result.region, null)
  assert.equal(result.regionAmbiguous, true)
  assert.ok(result.warnings.includes('region_mixed'))
})

run('no domain → default cn + region_unknown', () => {
  const result = detectImportBundle({
    format: 'workbuddy-accounts',
    accounts: [{ accessToken: 'a' }],
  })
  assert.equal(result.region, 'cn')
  assert.equal(result.regionAmbiguous, true)
  assert.ok(result.warnings.includes('region_unknown'))
})

run('shape accounts[] without format', () => {
  const result = detectImportBundle({
    accounts: [{ refreshToken: 'r', domain: 'codebuddy.cn' }],
  })
  assert.equal(result.provider, 'workbuddy')
  assert.equal(result.format, 'workbuddy-accounts')
  assert.equal(result.confidence, 'medium')
})

run('qoder shape', () => {
  const result = detectImportBundle({
    user_blob: 'x',
    machine_id: 'm',
  })
  assert.equal(result.provider, 'qoder')
  assert.equal(result.format, 'qoder-native-v1')
  assert.equal(result.batchImportSupported, false)
})

run('known other formats unsupported for batch', () => {
  for (const format of [
    'trae-oauth-v1',
    'devin-session-v1',
    'command-key-v1',
    'codex-oauth-v1',
    'zhipu-key-v1',
    'qwenwork-oauth-v1',
  ]) {
    const result = detectImportBundle({ format })
    assert.equal(result.batchImportSupported, false, format)
    assert.ok(result.provider, format)
  }
})

run('bad json throws', () => {
  assert.throws(() => parseImportBundleText('{'), /bad_json/)
  assert.throws(() => parseImportBundleText('[]'), /bad_json/)
})

run('unknown format field still shape-detects', () => {
  const result = detectImportBundle({
    format: 'mystery-v9',
    accounts: [{ accessToken: 'a', domain: 'codebuddy.cn' }],
  })
  assert.equal(result.provider, 'workbuddy')
  assert.ok(result.warnings.includes('unknown_format'))
})

run('exportType WorkBuddy-accounts alias → workbuddy-accounts', () => {
  const result = detectImportBundle({
    exportType: 'WorkBuddy-accounts',
    version: 1,
    count: 1,
    accounts: [
      {
        account: { uid: 'u-syn', nickname: 'Syn', phoneNumber: '13800000000', type: 'personal' },
        auth: {
          accessToken: 'syn-access-token',
          refreshToken: 'syn-refresh-token',
          tokenType: 'Bearer',
          expiresIn: 2592000,
          expiresAt: 1795766725050,
          domain: 'www.workbuddy.cn',
          scope: 'openid',
        },
      },
    ],
  })
  assert.equal(result.provider, 'workbuddy')
  assert.equal(result.format, 'workbuddy-accounts')
  assert.equal(result.region, 'cn')
  assert.equal(result.regionAmbiguous, false)
  assert.equal(result.batchImportSupported, true)
  assert.equal(result.confidence, 'high')
})

run('nested account/auth entry without format still detects', () => {
  const result = detectImportBundle({
    accounts: [
      {
        account: { uid: 'u2' },
        auth: { accessToken: 'a', domain: 'www.workbuddy.cn' },
      },
    ],
  })
  assert.equal(result.provider, 'workbuddy')
  assert.equal(result.format, 'workbuddy-accounts')
  assert.equal(result.region, 'cn')
  assert.equal(result.regionAmbiguous, false)
  assert.equal(result.batchImportSupported, true)
})

run('workbuddy.cn is CN; workbuddy.ai stays global', () => {
  const cn = detectImportBundle({
    format: 'workbuddy-accounts',
    accounts: [{ accessToken: 'a', domain: 'www.workbuddy.cn' }],
  })
  assert.equal(cn.region, 'cn')
  assert.equal(cn.regionAmbiguous, false)

  const global = detectImportBundle({
    format: 'workbuddy-accounts',
    accounts: [{ accessToken: 'a', domain: 'www.workbuddy.ai' }],
  })
  assert.equal(global.region, 'global')
  assert.equal(global.regionAmbiguous, false)
})

console.log('all detect tests passed')
