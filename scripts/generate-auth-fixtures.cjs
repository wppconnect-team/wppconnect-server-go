// Run with bcrypt 6.0.0, the version used by the Node reference server.
// BCRYPT_MODULE may point to an existing bcrypt installation.
const fs = require('node:fs');
const path = require('node:path');
const bcrypt = require(process.env.BCRYPT_MODULE || 'bcrypt');
const salt = '$2b$10$......................';
const cases = [
  { session: 'alpha', secret: 'test-secret' },
  { session: 'sessao', secret: 'segredo-com-acentos-áç' },
  { session: 'limit', secret: 'x'.repeat(67) },
];
const fixtures = cases.map(({ session, secret }) => {
  const raw = bcrypt.hashSync(session + secret, salt);
  const token = raw.replace(/\//g, '_').replace(/\+/g, '-');
  if (!bcrypt.compareSync(session + secret, raw)) throw new Error('Fixture verification failed');
  return { session, secret, raw, token, full: `${session}:${token}` };
});
const target = path.join(__dirname, '..', 'internal', 'auth', 'testdata', 'node-bcrypt.json');
fs.mkdirSync(path.dirname(target), { recursive: true });
fs.writeFileSync(target, JSON.stringify({ generator: 'Node bcrypt 6.0.0; test credentials only', fixtures }, null, 2) + '\n');
