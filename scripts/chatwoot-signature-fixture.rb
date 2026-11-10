require "active_record"
require "active_support/core_ext/securerandom"
require "active_support/core_ext/object/blank"
require "json"
require "openssl"

# Use the installed release's signer and Rails token generator. The serialized
# callback contains synthetic IDs and text. No external HTTP request is made.
module Webhooks
end
require "/app/lib/webhooks/trigger"
secret = ActiveRecord::Base.generate_unique_secure_token
body = JSON.generate({
  event: "message_created", id: 113, content: "Olá do Chatwoot", content_type: "text",
  message_type: "outgoing", private: false, source_id: nil, sender: {type: "user", id: 7},
  account: {id: 1}, inbox: {id: 2}, conversation: {id: 3, inbox_id: 2, meta: {sender: {id: 4}}}
})
signer = Webhooks::Trigger.new(nil, nil, :account_webhook, secret: secret, delivery_id: "fixture")
headers = signer.send(:request_headers, body)
puts JSON.generate({secret: secret, body: body, headers: headers})
