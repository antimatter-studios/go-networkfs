-- lighttpd offers plain MD5 in its Digest challenge, yet checks an answer
-- against whatever algorithm the answer names, MD5-sess included. This script
-- makes it an MD5-sess server, and changes nothing else:
--
--   * as a request arrives, an Authorization that answers Digest with any
--     algorithm but MD5-sess is refused 400, since that is not what this
--     server offers. Whether an MD5-sess answer is right is left to mod_auth.
--   * as a response starts, the challenge's algorithm=MD5 becomes MD5-sess.
--
-- mod_magnet runs it at both points (lighttpd.conf); lighty.r.resp_header is
-- empty while the request is still arriving.
local authorization = lighty.r.req_header["Authorization"]
local challenge = lighty.r.resp_header["WWW-Authenticate"]

if challenge then
  lighty.r.resp_header["WWW-Authenticate"] =
    challenge:gsub("algorithm=MD5,", "algorithm=MD5-sess,")
elseif authorization and authorization:lower():match("^%s*digest%s") then
  local algorithm = authorization:lower():match('[%s,]algorithm%s*=%s*"?([%w%-]+)')
  if algorithm ~= "md5-sess" then
    lighty.r.resp_body.set({ "this server offers Digest algorithm=MD5-sess only; the answer named "
      .. tostring(algorithm) .. "\n" })
    return 400
  end
end
