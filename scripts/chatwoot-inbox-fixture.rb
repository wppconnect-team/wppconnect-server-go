account = Account.create!(name: "Relay integration fixture")
user = User.new(name: "Local Agent", email: "agent@example.invalid", password: SecureRandom.hex(24) + "A3!")
user.skip_confirmation!
user.save!
AccountUser.create!(account: account, user: user, role: :administrator)
channel = Channel::Api.create!(account: account)
inbox = Inbox.create!(account: account, channel: channel, name: "Local API inbox")
contact = Contact.create!(account: account, name: "Synthetic contact")
link = ContactInbox.create!(inbox: inbox, contact: contact, source_id: "local-contact")
conversation = Conversation.create!(account: account, inbox: inbox, contact: contact, contact_inbox: link)
agent_reply = conversation.messages.create!(account: account, inbox: inbox, message_type: :outgoing, content: "agent fixture", sender: user)
secret = ActiveRecord::Base.generate_unique_secure_token
body = agent_reply.webhook_data.merge(event: "message_created").to_json
headers = Webhooks::Trigger.new(nil, nil, :account_webhook, secret: secret).send(:request_headers, body)
File.write("/fixture/state.json", {account: account.id, inbox: inbox.id, conversation: conversation.reload.display_id,
  contact: contact.id, token: user.reload.access_token.token, secret: secret, body: body, headers: headers}.to_json)
