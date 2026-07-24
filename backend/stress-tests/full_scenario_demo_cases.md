# Full Fraud Scenario Demo Cases

This document describes what the ITC full scenario catalog demo exercises in `demo_full_fraud_scenario_catalog.py`.

The demo seeds one shared historical dataset, then evaluates one transaction payload per case. A clean case is included for every scenario, followed by one case per rule and one full-risk case that is expected to trigger all rules in that scenario.

## Wallet Transfer Fraud Screening

| Case | What it tests | Expected rules | Key transaction setup |
| --- | --- | --- | --- |
| Clean Wallet Transfer | Normal wallet transfer with known account and device. | None | `account_ref=AcctQuiet`, `device_id=DevQuiet` |
| High Transfer Amount Only | Current amount is much larger than the account's historical average. | High Transfer Amount | `account_ref=AcctQuiet`, `amount=4000` |
| Product Limit Breach Only | Transfer amount exceeds the product limit. | Product Limit Breach | `product_id=ProductLowLimit`, `amount=6000` |
| One Hour Transfer Burst Only | Account already has enough recent transfers to breach the one-hour count rule. | One Hour Transfer Burst | `account_ref=AcctWalletFull`, `amount=500` |
| Weekly Transfer Velocity Only | Account already has enough weekly transfer value to breach the seven-day velocity rule. | Weekly Transfer Velocity | `account_ref=AcctWalletFull`, `amount=500` |
| New Device Transfer Only | Transfer is made from a device not known for the account. | New Device Transfer | `account_ref=AcctQuiet`, `device_id=DevNew` |
| Shared Device Risk Only | Device has recently been used by multiple accounts. | Shared Device Risk | `device_id=DevWalletShared`, `account_ref=AcctWalletFull` |
| Unusual IP Region Only | Transaction region differs from the account's usual region. | Unusual IP Region | `ip_region=Ashanti` |
| High Risk IP Only | Transaction IP is on the high-risk IP list. | High Risk IP | `ip=HIGH_RISK_IP` |
| New Beneficiary Only | Payment is sent to a recently created beneficiary above the rule amount. | New Beneficiary | `beneficiary_id=BeneficiaryNew`, `amount=2500` |
| Low KYC Only | Account KYC level is below the required level for the transfer. | Low KYC | `account_ref=AcctLowKyc`, `required_kyc_level=3` |
| Full Wallet Transfer Risk | Combines high amount, low product limit, burst/velocity history, shared device, unusual region, high-risk IP, new beneficiary, and low KYC. | All Wallet Transfer Fraud Screening rules | `account_ref=AcctWalletFull`, `device_id=DevWalletShared`, `product_id=ProductLowLimit`, `beneficiary_id=BeneficiaryNew`, `amount=6000`, `ip=HIGH_RISK_IP`, `ip_region=Ashanti`, `required_kyc_level=4` |

## Account Takeover Detection

| Case | What it tests | Expected rules | Key transaction setup |
| --- | --- | --- | --- |
| Clean Account Takeover Transfer | Normal transfer with no account takeover signal. | None | Default transaction fields |
| New Device Only | High-value transaction from a newly seen device. | New Device High Value Transaction | `device_id=DevNew`, `amount=6000` |
| Suspicious IP Only | Transaction comes from a high-risk IP. | Suspicious IP | `ip=HIGH_RISK_IP` |
| Impossible Travel Only | Current and previous IP locations are too far apart for the elapsed time. | Impossible Travel | `previous_ip_distance_km=800`, `previous_transaction_minutes_ago=60` |
| Post Credential Change Only | High-value transfer shortly after a password change. | Post Credential Change Transfer | `account_ref=AcctRecentPassword`, `amount=2500` |
| Failed Login Only | Recent failed login attempts exist before the transaction. | Failed Login Before Transaction | `account_ref=AcctFailedLogin` |
| New Beneficiary Only | Payment to a newly created beneficiary above the rule amount. | New Beneficiary After Login | `beneficiary_id=BeneficiaryNew`, `amount=1500` |
| Abnormal Spend Only | Amount is much higher than the account's historical average. | Abnormal Account Spend | `account_ref=AcctAbnormal`, `amount=400` |
| Rapid Post-Login Only | Account has more than five transactions in the recent post-login window. | Rapid Post-Login Transfers | `account_ref=AcctRapid` |
| Full Account Takeover Risk | Combines new device, suspicious IP, impossible travel, recent password change, failed logins, new beneficiary, abnormal spend, and rapid post-login transfers. | All Account Takeover Detection rules | `account_ref=AcctAtoFull`, `device_id=DevAtoFull`, `beneficiary_id=BeneficiaryNew`, `amount=6000`, `ip=HIGH_RISK_IP`, `previous_ip_distance_km=800`, `previous_transaction_minutes_ago=60` |

## Merchant Abuse Monitoring

| Case | What it tests | Expected rules | Key transaction setup |
| --- | --- | --- | --- |
| Clean Merchant Payment | Normal merchant payment. | None | Default transaction fields |
| High Weekly Merchant Volume Only | Merchant has exceeded the seven-day transaction value threshold. | High Weekly Merchant Volume | `merchant_id=MerchantHighWeekly` |
| Rapid Merchant Burst Only | Merchant has more than fifty transactions in the last hour. | Rapid Merchant Payment Burst | `merchant_id=MerchantBurst`, `amount=100` |
| Watchlisted Merchant Only | Merchant name fuzzy-matches the watchlist. | Watchlisted Merchant Name Match | `merchant_id=MerchantWatch` |
| Merchant Category Mismatch Only | Merchant category conflicts with product category. | Merchant Category Mismatch | `merchant_id=MerchantMismatch`, `product_id=ProductGrocery` |
| Repeated Same Account Payments Only | Same account has paid the same merchant too many times in 24 hours. | Repeated Same Account Payments | `merchant_id=MerchantRepeat` |
| Shared Merchant IP Cluster Only | Same IP is linked to too many merchants. | Shared Merchant IP Cluster | `merchant_id=MerchantShared`, `ip=102.88.1.77` |
| Abnormal Merchant Ticket Only | Current ticket is much higher than the merchant's historical average. | Abnormal Merchant Average Ticket | `merchant_id=MerchantAbnormal`, `amount=400` |
| Recent Settlement Change Only | Merchant settlement account was recently changed while volume is high. | Settlement Account Recently Changed | `merchant_id=MerchantSettlement` |
| Full Merchant Abuse Risk | Combines weekly volume, burst, watchlist, category mismatch, repeated account payments, shared IP cluster, abnormal ticket, and recent settlement change. | All Merchant Abuse Monitoring rules | `merchant_id=MerchantFull`, `product_id=ProductGrocery`, `account_ref=AcctMerchantFull`, `amount=120000`, `ip=102.88.1.77` |

## High Value Transaction Review

| Case | What it tests | Expected rules | Key transaction setup |
| --- | --- | --- | --- |
| Clean High Value Review Transfer | Normal transaction below high-value risk thresholds. | None | Default transaction fields |
| High Value Only | Amount is above the high-value threshold. | High Value Transaction | `amount=11000` |
| Product Limit Only | Amount is above product limit. | Product Limit Breach | `product_id=ProductLowLimit`, `amount=6000` |
| Low KYC Only | High-value amount requires higher KYC than the account has. | Low KYC High Value Transaction | `account_ref=AcctLowKyc`, `required_kyc_level=3`, `amount=6000` |
| New Device High Value Only | High-value transaction is made from a new device. | New Device High Value Transaction | `device_id=DevNew`, `amount=6000` |
| Unusual IP Only | Transaction uses high-risk IP. | Unusual IP High Value Transaction | `ip=HIGH_RISK_IP` |
| Abnormal High Value Spend Only | Amount is much higher than the account's historical average. | Abnormal High Value Spend | `account_ref=AcctAbnormal`, `amount=400` |
| Fast Outflow After Funding Only | Outgoing amount is large compared with recent incoming funding. | Fast Outflow After Funding | `account_ref=AcctFunding`, `amount=10000` |
| Recent Account Change Only | High-value transaction follows a recent profile change. | Recent Account Change High Value Transaction | `account_ref=AcctRecentProfile`, `amount=6000` |
| Full High Value Risk | Combines high value, product limit breach, low KYC, new device, suspicious IP, abnormal spend, fast outflow, and recent account change. | All High Value Transaction Review rules | `account_ref=AcctHvtFull`, `device_id=DevHvtFull`, `product_id=ProductLowLimit`, `beneficiary_id=BeneficiaryNew`, `amount=12000`, `ip=HIGH_RISK_IP`, `required_kyc_level=4` |

## Card Payment Authorization Risk

| Case | What it tests | Expected rules | Key transaction setup |
| --- | --- | --- | --- |
| Clean Card Authorization | Normal card payment. | None | Default transaction fields |
| High Card Payment Amount Only | Card payment amount exceeds the high card payment threshold. | High Card Payment Amount | `channel=card`, `amount=6000` |
| Product Limit Breach Only | Card payment amount exceeds product limit. | Product Limit Breach | `channel=card`, `product_id=ProductLowLimit`, `amount=6000` |
| High Risk Merchant Category Only | Card payment is to a high-risk merchant category. | High Risk Merchant Category | `channel=card`, `merchant_id=MerchantWatch`, `amount=500` |
| Card Testing Pattern Only | Account has repeated declined card transactions in the recent window. | Card Testing Pattern | `channel=card`, `account_ref=AcctCardFull`, `amount=500` |
| Small-To-Large Card Escalation Only | Several small card transactions are followed by a larger card payment. | Small-To-Large Card Escalation | `channel=card`, `account_ref=AcctCardFull`, `amount=1500` |
| Abnormal Card Spend Only | Card amount is much higher than the account's card payment average. | Abnormal Card Spend | `channel=card`, `account_ref=AcctCardFull`, `amount=6000` |
| Suspicious IP Card Payment Only | Card payment uses high-risk IP. | Suspicious IP Card Payment | `channel=card`, `ip=HIGH_RISK_IP` |
| Unusual Card Payment Hour Only | Card payment occurs outside usual active transaction hours. | Unusual Card Payment Hour | `channel=card`, `is_usual_active_hour=False` |
| Full Card Authorization Risk | Combines high amount, product limit, high-risk merchant category, card testing, small-to-large escalation, abnormal card spend, suspicious IP, and unusual hour. | All Card Payment Authorization Risk rules | `account_ref=AcctCardFull`, `merchant_id=MerchantWatch`, `product_id=ProductLowLimit`, `channel=card`, `amount=6000`, `ip=HIGH_RISK_IP`, `is_usual_active_hour=False` |

## Bank Transfer Risk Assessment

| Case | What it tests | Expected rules | Key transaction setup |
| --- | --- | --- | --- |
| Clean Bank Transfer | Normal bank transfer to known beneficiary. | None | `account_ref=AcctBankClean`, `beneficiary_id=BeneficiaryBankClean` |
| High Bank Transfer Amount Only | Bank transfer amount exceeds high bank transfer threshold. | High Bank Transfer Amount | `channel=bank`, `amount=11000` |
| Product Limit Breach Only | Bank transfer amount exceeds product limit. | Product Limit Breach | `channel=bank`, `product_id=ProductLowLimit`, `amount=6000` |
| New Beneficiary Only | High-value bank transfer to newly created beneficiary. | New Beneficiary; First Transfer To Beneficiary | `channel=bank`, `beneficiary_id=BeneficiaryNew`, `amount=6000` |
| First Transfer To Beneficiary Only | First bank transfer to a beneficiary is above the configured amount. | First Transfer To Beneficiary | `channel=bank`, `beneficiary_id=BeneficiaryNew`, `amount=2500` |
| Rapid Bank Transfer Burst Only | Account has more than five bank transfers in the recent hour. | Rapid Bank Transfer Burst | `channel=bank`, `account_ref=AcctBankFull`, `amount=500` |
| Post Account Change Bank Transfer Only | Bank transfer occurs shortly after account profile change. | Post Account Change Bank Transfer | `channel=bank`, `account_ref=AcctBankFull`, `amount=500` |
| Beneficiary Shared Across Many Accounts Only | Beneficiary has received transfers from too many accounts. | Beneficiary Shared Across Many Accounts | `channel=bank`, `beneficiary_id=BeneficiaryNew`, `amount=500` |
| Abnormal Bank Transfer Amount Only | Bank transfer amount is much higher than the account's bank-transfer average. | Abnormal Bank Transfer Amount | `channel=bank`, `account_ref=AcctBankFull`, `amount=6000` |
| Full Bank Transfer Risk | Combines high bank amount, product limit breach, new beneficiary, first transfer, burst, account change, shared beneficiary, and abnormal amount. | All Bank Transfer Risk Assessment rules | `account_ref=AcctBankFull`, `product_id=ProductLowLimit`, `beneficiary_id=BeneficiaryNew`, `channel=bank`, `amount=12000` |

## Cash-Out Fraud Monitoring

| Case | What it tests | Expected rules | Key transaction setup |
| --- | --- | --- | --- |
| Clean Cash-Out | Normal cash-out transaction. | None | `account_ref=AcctCashOutClean` |
| Fast Cash-Out After Funding Only | Current cash-out drains a large share of recent incoming funds. | Fast Cash-Out After Funding | `account_ref=AcctCashOutFull`, `system_type=cash_out`, `amount=5000` |
| Rapid Cash-Out Burst Only | Account has too many cash-outs in the last hour. | Rapid Cash-Out Burst | `account_ref=AcctCashOutFull`, `system_type=cash_out`, `amount=500` |
| High Cash-Out Amount Only | Cash-out amount exceeds the high cash-out threshold. | High Cash-Out Amount | `system_type=cash_out`, `amount=6000` |
| Agent High Daily Cash-Out Volume Only | Cash-out agent has high daily cash-out volume. | Agent High Daily Cash-Out Volume | `merchant_id=MerchantCashOutFull`, `system_type=cash_out`, `amount=500` |
| Agent Shared Across Many Accounts Only | Cash-out agent is used by many accounts in 24 hours. | Agent Shared Across Many Accounts | `merchant_id=MerchantCashOutFull`, `system_type=cash_out`, `amount=500` |
| Unusual Cash-Out Location Only | Cash-out region differs from normal region. | Unusual Cash-Out Location | `system_type=cash_out`, `ip_region=Ashanti` |
| Abnormal Cash-Out Amount Only | Cash-out amount is much higher than account's cash-out average. | Abnormal Cash-Out Amount | `account_ref=AcctCashOutFull`, `system_type=cash_out`, `amount=6000` |
| Low KYC Cash-Out Only | Account KYC level is below required cash-out KYC level. | Low KYC Cash-Out | `account_ref=AcctCashOutFull`, `system_type=cash_out`, `required_kyc_level=4` |
| Full Cash-Out Risk | Combines fast cash-out after funding, cash-out burst, high amount, high-volume agent, shared agent, unusual location, abnormal amount, and low KYC. | All Cash-Out Fraud Monitoring rules | `account_ref=AcctCashOutFull`, `merchant_id=MerchantCashOutFull`, `system_type=cash_out`, `amount=6000`, `ip_region=Ashanti`, `required_kyc_level=4` |

## New Beneficiary Payment Review

| Case | What it tests | Expected rules | Key transaction setup |
| --- | --- | --- | --- |
| Clean Beneficiary Payment | Normal payment to beneficiary. | None | Default transaction fields |
| New Beneficiary High Value Payment Only | High-value payment to newly created beneficiary. | New Beneficiary High Value Payment; First Payment To Beneficiary | `beneficiary_id=BeneficiaryNew`, `amount=6000` |
| First Payment To Beneficiary Only | First payment to beneficiary above the configured amount. | First Payment To Beneficiary | `beneficiary_id=BeneficiaryNew`, `amount=2500` |
| Rapid New Beneficiary Additions Only | Account has added too many beneficiaries recently. | Rapid New Beneficiary Additions | `account_ref=AcctNewBeneficiaryFull`, `beneficiary_id=BeneficiaryOld` |
| Beneficiary Watchlist Match Only | Beneficiary name or account reference fuzzy-matches watchlist. | Beneficiary Watchlist Match | `beneficiary_id=BeneficiaryWatch` |
| New Device Beneficiary Payment Only | Payment to new beneficiary from a new device. | New Device Beneficiary Payment | `beneficiary_id=BeneficiaryNew`, `device_id=DevNew` |
| Suspicious IP Beneficiary Payment Only | Payment to new beneficiary from high-risk IP. | Suspicious IP Beneficiary Payment | `beneficiary_id=BeneficiaryNew`, `ip=HIGH_RISK_IP` |
| Post Account Change Beneficiary Payment Only | Payment to new beneficiary after recent profile change. | Post Account Change Beneficiary Payment | `account_ref=AcctRecentProfile`, `beneficiary_id=BeneficiaryNew` |
| Abnormal First Beneficiary Payment Only | First-beneficiary payment amount is abnormally high for the account. | Abnormal First Beneficiary Payment | `account_ref=AcctNewBeneficiaryFull`, `beneficiary_id=BeneficiaryOld`, `amount=6000` |
| Full New Beneficiary Risk | Combines high-value new beneficiary, first payment, rapid additions, watchlist, new device, suspicious IP, account change, and abnormal first payment. | All New Beneficiary Payment Review rules | `account_ref=AcctNewBeneficiaryFull`, `device_id=DevNewBeneficiaryFull`, `beneficiary_id=BeneficiaryWatch`, `amount=6000`, `ip=HIGH_RISK_IP` |

## Dormant Account Reactivation Risk

| Case | What it tests | Expected rules | Key transaction setup |
| --- | --- | --- | --- |
| Clean Dormant Review Transfer | Normal transaction without dormancy risk. | None | Default transaction fields |
| Dormant Account Transaction Only | Dormant account performs a transaction above the base dormant threshold. | Dormant Account Transaction | `account_ref=AcctDormantFull`, `amount=1500` |
| High First Transaction After Dormancy Only | Dormant account makes first post-dormancy transaction above historical average. | Dormant Account Transaction; High First Transaction After Dormancy | `account_ref=AcctDormantFull`, `amount=2000` |
| New Device After Dormancy Only | Dormant account reactivates from a new device. | New Device After Dormancy | `account_ref=AcctDormantFull`, `device_id=DevDormantFull` |
| Suspicious IP After Dormancy Only | Dormant account reactivates from high-risk IP. | Suspicious IP After Dormancy | `account_ref=AcctDormantFull`, `ip=HIGH_RISK_IP` |
| Account Change After Dormancy Only | Dormant account has recent profile change. | Account Change After Dormancy | `account_ref=AcctDormantFull` |
| New Beneficiary After Dormancy Only | Dormant account pays newly created beneficiary. | New Beneficiary After Dormancy | `account_ref=AcctDormantFull`, `beneficiary_id=BeneficiaryNew` |
| Rapid Transfers After Dormancy Only | Dormant account has too many transfers in one hour after reactivation. | Rapid Transfers After Dormancy | `account_ref=AcctDormantFull` |
| Balance Drain After Dormancy Only | Dormant account sends a large share of available balance. | Dormant Account Transaction; High First Transaction After Dormancy; Balance Drain After Dormancy | `account_ref=AcctDormantFull`, `amount=9000` |
| Full Dormant Reactivation Risk | Combines dormancy transaction, high first transaction, new device, suspicious IP, account change, new beneficiary, rapid transfers, and balance drain. | All Dormant Account Reactivation Risk rules | `account_ref=AcctDormantFull`, `device_id=DevDormantFull`, `beneficiary_id=BeneficiaryNew`, `amount=9000`, `ip=HIGH_RISK_IP` |

## Cross-Border or Proxy Access Review

| Case | What it tests | Expected rules | Key transaction setup |
| --- | --- | --- | --- |
| Clean Cross-Border Review Transfer | Normal domestic access. | None | `ip=102.176.10.200` |
| IP Country Mismatch Only | IP country differs from account country. | IP Country Mismatch | `ip_country=NG` |
| High Risk Network Access Only | IP network is marked proxy or otherwise risky. | High Risk Network Access | `ip_network_risk=proxy` |
| Impossible Travel Access Only | Current and previous IP locations are too far apart for elapsed time. | Impossible Travel Access | `previous_ip_distance_km=800`, `previous_transaction_minutes_ago=60` |
| New Device Foreign Access Only | Foreign access occurs from a new device. | IP Country Mismatch; New Device Foreign Access | `ip_country=NG`, `device_id=DevNew` |
| High Value Foreign Access Transaction Only | Foreign access is paired with high-value transaction. | IP Country Mismatch; High Value Foreign Access Transaction | `ip_country=NG`, `amount=6000` |
| Foreign Access Behavior Change Only | Foreign access transaction amount is abnormal for the account. | IP Country Mismatch; Foreign Access Behavior Change | `account_ref=AcctCrossBorderFull`, `ip_country=NG`, `amount=6000` |
| Shared Suspicious IP Across Accounts Only | Same suspicious IP is shared by too many accounts. | Shared Suspicious IP Across Accounts | `ip=185.220.101.77` |
| Cross-Border Rapid Transaction Only | Foreign access account has rapid transaction velocity. | IP Country Mismatch; Cross-Border Rapid Transaction | `account_ref=AcctCrossBorderFull`, `ip_country=NG` |
| Full Cross-Border Proxy Risk | Combines country mismatch, high-risk network, impossible travel, new foreign device, high-value foreign transaction, behavior change, shared IP, and rapid transaction velocity. | All Cross-Border or Proxy Access Review rules | `account_ref=AcctCrossBorderFull`, `device_id=DevCrossBorderFull`, `amount=6000`, `ip=185.220.101.77`, `ip_country=NG`, `ip_network_risk=proxy`, `previous_ip_distance_km=800`, `previous_transaction_minutes_ago=60` |

## Chango Group Contribution Fraud Monitoring

| Case | What it tests | Expected rules | Key transaction setup |
| --- | --- | --- | --- |
| Clean Chango Contribution | Normal Chango group contribution. | None | `group_id=GroupQuiet`, `ip=102.176.10.201` |
| High Weekly Group Contribution Value Only | Group has exceeded the seven-day contribution value threshold. | High Weekly Group Contribution Value | `group_id=GroupChangoFull` |
| Rapid Group Contribution Burst Only | Group has more than twenty-five contributions in one hour. | Rapid Group Contribution Burst | `group_id=GroupChangoFull` |
| New Account Contribution Spike Only | New account contributes repeatedly. | New Account Contribution Spike | `account_ref=AcctChangoContributionFull`, `group_id=GroupQuiet` |
| Low KYC High Contribution Only | Contributor KYC is below required KYC level. | Low KYC High Contribution | `account_ref=AcctChangoContributionFull`, `required_kyc_level=4` |
| Shared IP Contributor Cluster Only | Same IP is used by too many contributors. | Shared IP Contributor Cluster | `ip=102.129.50.44`, `group_id=GroupQuiet` |
| Watchlisted Campaign Name Match Only | Group/campaign name fuzzy-matches campaign watchlist. | Watchlisted Campaign Name Match | `group_name=Fake Medical Emergency` |
| Threshold Structuring Pattern Only | Group has many contributions just below review threshold. | Threshold Structuring Pattern | `group_id=GroupChangoFull` |
| Abnormal Contribution Amount Only | Contribution amount is much higher than the account's average. | Abnormal Contribution Amount | `account_ref=AcctChangoContributionFull`, `amount=6000` |
| Full Chango Contribution Risk | Combines weekly group value, burst, new-account spike, low KYC, shared IP cluster, watchlisted campaign, structuring pattern, and abnormal contribution amount. | All Chango Group Contribution Fraud Monitoring rules | `account_ref=AcctChangoContributionFull`, `device_id=DevChangoContributionFull`, `group_id=GroupChangoFull`, `group_name=Fake Medical Emergency`, `system_type=contribution`, `amount=6000`, `ip=102.129.50.44`, `required_kyc_level=4` |

## Chango Disbursement and Borrowing Risk Review

| Case | What it tests | Expected rules | Key transaction setup |
| --- | --- | --- | --- |
| Clean Chango Disbursement | Normal Chango disbursement or borrowing review. | None | Default transaction fields |
| Missing Vote Approval Only | Disbursement is attempted without approved vote status. | Missing Vote Approval | `system_type=disbursement`, `vote_approval_status=pending` |
| Insufficient Approved Votes Only | Approved vote count is below the group's required vote count. | Insufficient Approved Votes | `approved_vote_count=1`, `required_vote_count=3` |
| Watchlisted Destination Match Only | Destination account matches watchlist. | Watchlisted Destination Match | `destination_account_ref=BlockedDestination` |
| Public Group Destination Mismatch Only | Public group destination does not match verified settlement account. | Public Group Destination Mismatch | `group_type=public`, `destination_account_ref=OtherDestination`, `verified_settlement_account_ref=VerifiedDestination` |
| Fast Cashout After Contribution Spike Only | Disbursement follows a recent contribution spike into the group. | Fast Cashout After Contribution Spike | `group_id=GroupDisbursementFull`, `system_type=disbursement` |
| High Group Balance Withdrawal Only | Group withdrawals exceed 80 percent of group balance. | High Group Balance Withdrawal | `group_id=GroupDisbursementFull`, `group_current_balance=10000` |
| Borrowing Above Limit Only | Outstanding loan exposure is above account borrowing limit. | Borrowing Above Limit | `account_ref=AcctChangoDisbursementFull`, `outstanding_loan_amount=2000` |
| New Member High Loan Request Only | New group member requests a high loan. | New Member High Loan Request | `member_joined_at=BASE_TIME - 2 days`, `loan_amount=1500` |
| Full Chango Disbursement Risk | Combines missing approval, insufficient votes, watchlisted destination, public destination mismatch, contribution spike cashout, high balance withdrawal, borrowing above limit, and new-member loan request. | All Chango Disbursement and Borrowing Risk Review rules | `account_ref=AcctChangoDisbursementFull`, `group_id=GroupDisbursementFull`, `system_type=disbursement`, `amount=1500`, `vote_approval_status=pending`, `approved_vote_count=1`, `required_vote_count=3`, `destination_account_ref=BlockedDestination`, `group_type=public`, `group_current_balance=10000`, `outstanding_loan_amount=2000`, `loan_amount=1500`, `loan_status=active` |

## Regulatory Reporting Review

| Case | What it tests | Expected rules | Key transaction setup |
| --- | --- | --- | --- |
| Clean Regulatory Review | Transaction does not meet any regulatory reporting trigger. | None | Default transaction fields |
| Cash Transaction Threshold Report | Cash or branch-cash GHS transaction meets the reporting threshold. | Cash Transaction Threshold Report | `channel=branch_cash`, `currency=GHS`, `amount=50000` |
| Suspicious Low-Value Repeated Activity | Low-value repeated activity exists for the same account. | Suspicious Low-Value Repeated Activity | `account_ref=AcctRegulatoryStr`, `amount=100` |
| Cross-Border Currency Declaration Review | Cross-border cash declaration occurs through airport or land border. | Cross-Border Currency Declaration Review | `system_type=cross_border_cash_declaration`, `country=US`, `entry_point_type=airport` |
| Electronic Transfer Reporting Threshold | USD inward or outward electronic/bank transfer exceeds reporting threshold. | Electronic Transfer Reporting Threshold | `channel=bank`, `currency=USD`, `amount=2000`, `direction=outward` |

## Case Count Summary

| Scenario | Cases |
| --- | ---: |
| Wallet Transfer Fraud Screening | 12 |
| Account Takeover Detection | 10 |
| Merchant Abuse Monitoring | 10 |
| High Value Transaction Review | 10 |
| Card Payment Authorization Risk | 10 |
| Bank Transfer Risk Assessment | 10 |
| Cash-Out Fraud Monitoring | 10 |
| New Beneficiary Payment Review | 10 |
| Dormant Account Reactivation Risk | 10 |
| Cross-Border or Proxy Access Review | 10 |
| Chango Group Contribution Fraud Monitoring | 10 |
| Chango Disbursement and Borrowing Risk Review | 10 |
| Regulatory Reporting Review | 5 |
| Total | 127 |
