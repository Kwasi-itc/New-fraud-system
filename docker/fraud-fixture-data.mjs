import { createHash } from 'node:crypto';

const FIRST_NAMES = ['Akosua', 'Kwame', 'Ama', 'Kofi', 'Abena', 'Yaw', 'Efua', 'Kojo', 'Adwoa', 'Kwaku', 'Esi', 'Kwesi', 'Akua', 'Kwadwo', 'Afia', 'Nana', 'Amina', 'Yussif', 'Zainab', 'Abdul'];
const SURNAMES = ['Mensah', 'Owusu', 'Boateng', 'Asante', 'Osei', 'Adjei', 'Agyeman', 'Appiah', 'Addo', 'Antwi', 'Asamoah', 'Bonsu', 'Darko', 'Frimpong', 'Opoku', 'Amponsah', 'Sarpong', 'Tetteh', 'Quartey', 'Abubakar'];
const NETWORKS = [{ name: 'MTN', prefix: '24' }, { name: 'Telecel', prefix: '20' }, { name: 'AT', prefix: '27' }];

export function fixtureHash(value) {
  return createHash('sha256').update(`fraud-simulation/v1/${value}`).digest('hex');
}

// Reproducible synthetic identities, never sourced from customer records. A
// stable user ID produces the same contact details in every related fixture.
export function fixturePerson(identity) {
  const digest = Buffer.from(fixtureHash(`person/${identity}`), 'hex');
  const first = FIRST_NAMES[digest.readUInt16BE(0) % FIRST_NAMES.length];
  const last = SURNAMES[digest.readUInt16BE(2) % SURNAMES.length];
  const network = NETWORKS[digest[4] % NETWORKS.length];
  const subscriber = String(digest.readUInt32BE(5) % 10000000).padStart(7, '0');
  const phone = `+233${network.prefix}${subscriber}`;
  return {
    name: `${first} ${last}`, first, last, phone,
    maskedPhone: `+233 ${network.prefix} *** ${subscriber.slice(-4)}`,
    email: `${first.toLowerCase()}.${last.toLowerCase()}.${subscriber.slice(-4)}@example.test`,
    maskedEmail: `${first[0].toLowerCase()}***.${last.toLowerCase()}@example.test`,
    authType: digest[9] % 3 === 0 ? 'email' : 'mobile', network: network.name,
  };
}

function walletIdentity(recordId, record) {
  const instrument = record.wallet_number_hash || fixtureHash(`wallet/${recordId}`);
  const digest = Buffer.from(fixtureHash(instrument), 'hex');
  const network = NETWORKS[digest[0] % NETWORKS.length];
  return { hash: instrument, mask: `+233 ${network.prefix} *** ${String(digest.readUInt32BE(1) % 10000).padStart(4, '0')}`, network: network.name };
}

export function enrichFixtureRecord(table, recordId, record, overrides) {
  const person = fixturePerson(record.user_id || recordId);
  let fields = {};
  if (table.name === 'customers') {
    const authType = overrides.auth_type ?? person.authType;
    const username = authType === 'email' ? person.email : person.phone;
    fields = {
      display_name: person.name, auth_type: authType,
      username_masked: authType === 'email' ? person.maskedEmail : person.maskedPhone,
      username_hash: fixtureHash(`username/${username}`), country_id: 'GH',
      status: record.days_since_last_activity > 90 ? 'dormant' : 'active',
      kyc_tier: record.has_identity_number ? 'verified' : 'basic',
      distinct_devices: 1 + parseInt(fixtureHash(recordId).slice(0, 2), 16) % 3,
    };
  } else if (table.name === 'wallets') {
    const instrument = walletIdentity(recordId, overrides);
    const other = fixturePerson(`${record.user_id}/other-holder`);
    const differentName = other.name !== person.name ? other.name : person.name === 'Ama Boateng' ? 'Kofi Mensah' : 'Ama Boateng';
    fields = {
      wallet_number_masked: instrument.mask, wallet_number_hash: instrument.hash,
      financial_entity_id: `GH-MOMO-${instrument.network.toUpperCase()}`, country_id: 'GH',
      account_holder_name: person.name,
      name_enquiry_name: record.name_match === 'unknown' ? null : record.name_match === 'none' ? differentName : record.name_match === 'partial' ? `${person.first[0]}. ${person.last}` : person.name,
    };
  } else if (table.name === 'transactions') {
    const destination = fixturePerson(record.destination_ref || recordId);
    const wallet = walletIdentity(record.wallet_id || recordId, {});
    const reference = fixtureHash(`payment/${recordId}`).slice(0, 12).toUpperCase();
    const responses = {
      settled: ['00', 'Transfer completed successfully'],
      callback_received: ['00', 'Provider callback received'],
      failed: ['05', 'Transfer declined by provider'],
      blocked: ['REVIEW', 'Transfer held for review'],
      undetermined: ['PENDING', 'Awaiting reconciliation'],
    };
    const response = responses[record.state];
    fields = {
      source_msisdn: person.phone, destination_network: destination.network,
      funding_network: `GH-MOMO-${wallet.network.toUpperCase()}`,
      merchant_product_id: 'mobile-money-transfer', payment_ref: `PAY-GH-${reference}`,
      original_amount: record.amount,
      provider_transaction_id: response ? `MOMO-${reference}` : null,
      response_code: response?.[0] ?? null, response_message: response?.[1] ?? null,
    };
  }
  const available = new Map(table.fields.filter(field => !field.archived).map(field => [field.name, field]));
  for (const [name, value] of Object.entries(fields)) {
    const field = available.get(name);
    if (!field || Object.hasOwn(overrides, name)) continue;
    if (field.is_enum && !field.enum_values?.some(option => option.value === value)) continue;
    if (value == null && !field.nullable) continue;
    record[name] = value;
  }
  return record;
}
